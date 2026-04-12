package agentbuilder

import (
	"errors"
	"fmt"
	"strings"

	"github.com/codalotl/codalotl/internal/llmstream"
)

const (
	yamlPresenterPresetSubagentQA = "subagent_q_and_a"
	yamlPresenterBodyNone         = "-"
	yamlPresenterBodyResult       = "result"
)

type yamlPresenterSpec struct {
	Preset *yamlPresenterPresetSpec `yaml:"preset"`
}

type yamlPresenterPresetSpec struct {
	Name         string                         `yaml:"name"`
	CallAction   string                         `yaml:"call_action"`
	ResultAction string                         `yaml:"result_action"`
	SummaryItems []yamlPresenterSummaryItemSpec `yaml:"summary_items"`
	CallBody     string                         `yaml:"call_body"`
	ResultBody   string                         `yaml:"result_body"`
}

type yamlPresenterSummaryItemSpec struct {
	Text  string `yaml:"text"`
	Param string `yaml:"param"`
}

type yamlNormalizedPresenterSpec struct {
	SubagentQA *yamlNormalizedSubagentQAPresenterSpec
}

type yamlNormalizedSubagentQAPresenterSpec struct {
	CallAction   string
	ResultAction string
	SummaryItems []yamlNormalizedPresenterSummaryItem
	CallBody     string
	ResultBody   string
}

type yamlNormalizedPresenterSummaryItem struct {
	Text  string
	Param string
}

type yamlSubagentQAPresenter struct {
	paramSpecs map[string]yamlNormalizedParameter
	spec       yamlNormalizedSubagentQAPresenterSpec
}

var _ llmstream.Presenter = (*yamlSubagentQAPresenter)(nil)

func normalizeYAMLPresenterSpec(spec *yamlPresenterSpec, params map[string]yamlNormalizedParameter) (*yamlNormalizedPresenterSpec, error) {
	if spec == nil {
		return nil, nil
	}
	if spec.Preset == nil {
		return nil, errors.New("presenter.preset is required")
	}

	name := strings.TrimSpace(spec.Preset.Name)
	switch name {
	case yamlPresenterPresetSubagentQA:
		normalized, err := normalizeYAMLSubagentQAPresenterSpec(spec.Preset, params)
		if err != nil {
			return nil, err
		}
		return &yamlNormalizedPresenterSpec{
			SubagentQA: &normalized,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported presenter preset %q", spec.Preset.Name)
	}
}

func normalizeYAMLSubagentQAPresenterSpec(spec *yamlPresenterPresetSpec, params map[string]yamlNormalizedParameter) (yamlNormalizedSubagentQAPresenterSpec, error) {
	callAction := strings.TrimSpace(spec.CallAction)
	if callAction == "" {
		return yamlNormalizedSubagentQAPresenterSpec{}, errors.New("presenter.preset.call_action is required")
	}

	resultAction := strings.TrimSpace(spec.ResultAction)
	if resultAction == "" {
		return yamlNormalizedSubagentQAPresenterSpec{}, errors.New("presenter.preset.result_action is required")
	}

	summaryItems := make([]yamlNormalizedPresenterSummaryItem, 0, len(spec.SummaryItems))
	for i, item := range spec.SummaryItems {
		normalized, err := normalizeYAMLPresenterSummaryItem(item, params)
		if err != nil {
			return yamlNormalizedSubagentQAPresenterSpec{}, fmt.Errorf("presenter.preset.summary_items[%d]: %w", i, err)
		}
		summaryItems = append(summaryItems, normalized)
	}

	callBody := strings.TrimSpace(spec.CallBody)
	switch callBody {
	case "":
		return yamlNormalizedSubagentQAPresenterSpec{}, errors.New("presenter.preset.call_body is required")
	case yamlPresenterBodyNone:
	default:
		if _, ok := params[callBody]; !ok {
			return yamlNormalizedSubagentQAPresenterSpec{}, fmt.Errorf("presenter.preset.call_body refers to unknown parameter %q", callBody)
		}
	}

	resultBody := strings.TrimSpace(spec.ResultBody)
	switch resultBody {
	case yamlPresenterBodyResult, yamlPresenterBodyNone:
	default:
		if resultBody == "" {
			return yamlNormalizedSubagentQAPresenterSpec{}, errors.New("presenter.preset.result_body is required")
		}
		return yamlNormalizedSubagentQAPresenterSpec{}, fmt.Errorf("unsupported presenter.preset.result_body %q", spec.ResultBody)
	}

	return yamlNormalizedSubagentQAPresenterSpec{
		CallAction:   callAction,
		ResultAction: resultAction,
		SummaryItems: summaryItems,
		CallBody:     callBody,
		ResultBody:   resultBody,
	}, nil
}

func normalizeYAMLPresenterSummaryItem(spec yamlPresenterSummaryItemSpec, params map[string]yamlNormalizedParameter) (yamlNormalizedPresenterSummaryItem, error) {
	text := strings.TrimSpace(spec.Text)
	param := strings.TrimSpace(spec.Param)

	switch {
	case text != "" && param != "":
		return yamlNormalizedPresenterSummaryItem{}, errors.New("must set exactly one of text or param")
	case text == "" && param == "":
		return yamlNormalizedPresenterSummaryItem{}, errors.New("must set exactly one of text or param")
	case text != "":
		return yamlNormalizedPresenterSummaryItem{Text: text}, nil
	default:
		if _, ok := params[param]; !ok {
			return yamlNormalizedPresenterSummaryItem{}, fmt.Errorf("param %q is not defined", param)
		}
		return yamlNormalizedPresenterSummaryItem{Param: param}, nil
	}
}

func buildYAMLPresenter(spec *yamlNormalizedPresenterSpec, paramSpecs map[string]yamlNormalizedParameter) llmstream.Presenter {
	if spec == nil {
		return nil
	}
	if spec.SubagentQA != nil {
		return &yamlSubagentQAPresenter{
			paramSpecs: paramSpecs,
			spec:       *spec.SubagentQA,
		}
	}
	return nil
}

func (p *yamlSubagentQAPresenter) Present(call llmstream.ToolCall, result *llmstream.ToolResult) llmstream.Presentation {
	params, err := parseYAMLToolCallParams(call.Input, p.paramSpecs)
	if err != nil {
		params = nil
	}

	presentation := llmstream.Presentation{
		Behavior:      llmstream.CompletionBehaviorAppend,
		ErrorBehavior: llmstream.ErrorBehaviorDefault,
		Summary:       p.summary(params, result != nil),
	}

	if result == nil {
		if body, ok := p.callBody(params); ok {
			presentation.Body = body
		}
		return presentation
	}

	if body, ok := p.resultBody(*result); ok {
		presentation.Body = body
	}
	return presentation
}

func (p *yamlSubagentQAPresenter) summary(params map[string]any, isResult bool) llmstream.Line {
	action := p.spec.CallAction
	if isResult {
		action = p.spec.ResultAction
	}

	segments := []llmstream.Segment{{
		Text: action,
		Role: llmstream.RoleAction,
	}}
	for _, item := range p.spec.SummaryItems {
		if item.Text != "" {
			segments = append(segments, llmstream.Segment{
				Text: item.Text,
				Role: llmstream.RoleAccent,
			})
			continue
		}

		value := yamlPresenterParamText(params, item.Param)
		if value == "" {
			continue
		}
		segments = append(segments, llmstream.Segment{
			Text: value,
			Role: llmstream.RoleNormal,
		})
	}

	return llmstream.Line{
		JoinWithSpace: true,
		Segments:      segments,
	}
}

func (p *yamlSubagentQAPresenter) callBody(params map[string]any) (llmstream.Output, bool) {
	if p.spec.CallBody == yamlPresenterBodyNone {
		return llmstream.Output{}, false
	}
	return yamlPresenterOutput(yamlPresenterParamText(params, p.spec.CallBody))
}

func (p *yamlSubagentQAPresenter) resultBody(result llmstream.ToolResult) (llmstream.Output, bool) {
	if p.spec.ResultBody != yamlPresenterBodyResult {
		return llmstream.Output{}, false
	}
	return yamlPresenterOutput(result.Result)
}

func yamlPresenterParamText(params map[string]any, name string) string {
	if params == nil || name == "" {
		return ""
	}

	value, ok := params[name]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func yamlPresenterOutput(content string) (llmstream.Output, bool) {
	content = strings.TrimSpace(content)
	if content == "" {
		return llmstream.Output{}, false
	}

	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	return llmstream.Output{
		Lines: strings.Split(content, "\n"),
	}, true
}
