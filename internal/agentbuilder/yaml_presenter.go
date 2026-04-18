package agentbuilder

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/codalotl/codalotl/internal/llmstream"
)

const (
	yamlPresenterPresetSubagentQA = "subagent_q_and_a"
	yamlPresenterPresetReview     = "review"
	yamlPresenterBodyNone         = "-"
	yamlPresenterBodyResult       = "result"
	yamlReviewBodyNoFindings      = "No actionable findings."
	yamlReviewMaxFindings         = 10
	yamlReviewBodyMoreFormat      = "... +%d findings"
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
	Review     *yamlNormalizedReviewPresenterSpec
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

type yamlNormalizedReviewPresenterSpec struct{}

type yamlSubagentQAPresenter struct {
	paramSpecs map[string]yamlNormalizedParameter
	spec       yamlNormalizedSubagentQAPresenterSpec
}

type yamlReviewPresenter struct {
	paramSpecs map[string]yamlNormalizedParameter
}

var _ llmstream.Presenter = (*yamlSubagentQAPresenter)(nil)
var _ llmstream.Presenter = (*yamlReviewPresenter)(nil)
var _ llmstream.SubagentFinalMessagePresenter = (*yamlSubagentQAPresenter)(nil)
var _ llmstream.SubagentFinalMessagePresenter = (*yamlReviewPresenter)(nil)

type yamlReviewResult struct {
	Findings               *[]yamlReviewFinding `json:"findings"`
	OverallCorrectness     string               `json:"overall_correctness"`
	OverallExplanation     string               `json:"overall_explanation"`
	OverallConfidenceScore *float64             `json:"overall_confidence_score"`
}

type yamlReviewFinding struct {
	Title           string                  `json:"title"`
	Body            string                  `json:"body"`
	ConfidenceScore *float64                `json:"confidence_score"`
	Priority        *int                    `json:"priority,omitempty"`
	CodeLocation    *yamlReviewCodeLocation `json:"code_location"`
}

type yamlReviewCodeLocation struct {
	AbsoluteFilePath string               `json:"absolute_file_path"`
	LineRange        *yamlReviewLineRange `json:"line_range"`
}

type yamlReviewLineRange struct {
	Start *int `json:"start"`
	End   *int `json:"end"`
}

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
	case yamlPresenterPresetReview:
		normalized, err := normalizeYAMLReviewPresenterSpec(spec.Preset, params)
		if err != nil {
			return nil, err
		}
		return &yamlNormalizedPresenterSpec{
			Review: &normalized,
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

func normalizeYAMLReviewPresenterSpec(spec *yamlPresenterPresetSpec, params map[string]yamlNormalizedParameter) (yamlNormalizedReviewPresenterSpec, error) {
	if _, ok := params["base"]; !ok {
		return yamlNormalizedReviewPresenterSpec{}, errors.New(`presenter.preset.name "review" requires parameter "base"`)
	}

	if strings.TrimSpace(spec.CallAction) != "" {
		return yamlNormalizedReviewPresenterSpec{}, errors.New(`presenter.preset.name "review" does not support call_action`)
	}
	if strings.TrimSpace(spec.ResultAction) != "" {
		return yamlNormalizedReviewPresenterSpec{}, errors.New(`presenter.preset.name "review" does not support result_action`)
	}
	if len(spec.SummaryItems) > 0 {
		return yamlNormalizedReviewPresenterSpec{}, errors.New(`presenter.preset.name "review" does not support summary_items`)
	}
	if strings.TrimSpace(spec.CallBody) != "" {
		return yamlNormalizedReviewPresenterSpec{}, errors.New(`presenter.preset.name "review" does not support call_body`)
	}
	if strings.TrimSpace(spec.ResultBody) != "" {
		return yamlNormalizedReviewPresenterSpec{}, errors.New(`presenter.preset.name "review" does not support result_body`)
	}

	return yamlNormalizedReviewPresenterSpec{}, nil
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
	if spec.Review != nil {
		return &yamlReviewPresenter{
			paramSpecs: paramSpecs,
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

func (p *yamlSubagentQAPresenter) SubagentFinalMessage(llmstream.ToolCall, string, string) llmstream.Block {
	return nil
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

func (p *yamlReviewPresenter) Present(call llmstream.ToolCall, result *llmstream.ToolResult) llmstream.Presentation {
	params, err := parseYAMLToolCallParams(call.Input, p.paramSpecs)
	if err != nil {
		params = nil
	}

	presentation := llmstream.Presentation{
		Behavior:      llmstream.CompletionBehaviorAppend,
		ErrorBehavior: llmstream.ErrorBehaviorDefault,
		Summary:       yamlReviewSummary(params, result != nil),
	}

	if result == nil {
		return presentation
	}

	if body, ok := yamlReviewResultBody(result.Result); ok {
		presentation.Body = body
	}
	return presentation
}

func (p *yamlReviewPresenter) SubagentFinalMessage(llmstream.ToolCall, string, string) llmstream.Block {
	return nil
}

func yamlReviewSummary(params map[string]any, isResult bool) llmstream.Line {
	action := "Reviewing"
	if isResult {
		action = "Reviewed"
	}

	segments := []llmstream.Segment{{
		Text: action,
		Role: llmstream.RoleAction,
	}}
	if base := yamlPresenterParamText(params, "base"); base != "" {
		segments = append(segments, llmstream.Segment{
			Text: base,
			Role: llmstream.RoleNormal,
		})
	}

	return llmstream.Line{
		JoinWithSpace: true,
		Segments:      segments,
	}
}

func yamlReviewResultBody(raw string) (llmstream.Output, bool) {
	review, ok := parseYAMLReviewResult(raw)
	if !ok {
		return yamlPresenterOutput(raw)
	}

	findings := *review.Findings
	if len(findings) == 0 {
		return llmstream.Output{
			Lines: []string{yamlReviewBodyNoFindings},
		}, true
	}

	limit := len(findings)
	if limit > yamlReviewMaxFindings {
		limit = yamlReviewMaxFindings
	}

	lines := make([]string, 0, limit+1)
	for _, finding := range findings[:limit] {
		lines = append(lines, finding.Title)
	}
	if remaining := len(findings) - limit; remaining > 0 {
		lines = append(lines, fmt.Sprintf(yamlReviewBodyMoreFormat, remaining))
	}

	return llmstream.Output{Lines: lines}, true
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

func parseYAMLReviewResult(raw string) (yamlReviewResult, bool) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()

	var parsed yamlReviewResult
	if err := decoder.Decode(&parsed); err != nil {
		return yamlReviewResult{}, false
	}

	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return yamlReviewResult{}, false
	}

	if !validateYAMLReviewResult(parsed) {
		return yamlReviewResult{}, false
	}
	return parsed, true
}

func validateYAMLReviewResult(result yamlReviewResult) bool {
	if result.Findings == nil {
		return false
	}
	switch result.OverallCorrectness {
	case "patch is correct", "patch is incorrect":
	default:
		return false
	}
	if strings.TrimSpace(result.OverallExplanation) == "" {
		return false
	}
	if !yamlReviewScoreValid(result.OverallConfidenceScore) {
		return false
	}

	for _, finding := range *result.Findings {
		if !validateYAMLReviewFinding(finding) {
			return false
		}
	}
	return true
}

func validateYAMLReviewFinding(finding yamlReviewFinding) bool {
	if strings.TrimSpace(finding.Title) == "" || strings.TrimSpace(finding.Body) == "" {
		return false
	}
	if !yamlReviewScoreValid(finding.ConfidenceScore) {
		return false
	}
	if finding.Priority != nil && (*finding.Priority < 0 || *finding.Priority > 3) {
		return false
	}
	if finding.CodeLocation == nil {
		return false
	}
	if !filepath.IsAbs(finding.CodeLocation.AbsoluteFilePath) {
		return false
	}
	if finding.CodeLocation.LineRange == nil {
		return false
	}
	if finding.CodeLocation.LineRange.Start == nil || finding.CodeLocation.LineRange.End == nil {
		return false
	}
	if *finding.CodeLocation.LineRange.Start <= 0 || *finding.CodeLocation.LineRange.End < *finding.CodeLocation.LineRange.Start {
		return false
	}
	return true
}

func yamlReviewScoreValid(score *float64) bool {
	return score != nil && *score >= 0 && *score <= 1
}
