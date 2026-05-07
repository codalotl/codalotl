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
	yamlReviewBodyMoreFormat      = "\u2026 +%d findings"
)

// A yamlPresenterSpec configures presentation behavior for a YAML-defined tool.
type yamlPresenterSpec struct {
	Preset *yamlPresenterPresetSpec `yaml:"preset"` // Preset selects the preset-based presenter configuration.
}

// A yamlPresenterPresetSpec configures a preset-based presenter for a YAML-defined tool.
type yamlPresenterPresetSpec struct {
	Name         string                         `yaml:"name"`          // Name selects the presenter preset.
	CallAction   string                         `yaml:"call_action"`   // CallAction is the action text shown while a supported preset is in progress.
	ResultAction string                         `yaml:"result_action"` // ResultAction is the action text shown after a supported preset completes.
	SummaryItems []yamlPresenterSummaryItemSpec `yaml:"summary_items"` // SummaryItems are ordered literal and parameter segments appended to the summary.
	CallBody     string                         `yaml:"call_body"`     // CallBody selects the source of the body shown for the tool call.
	ResultBody   string                         `yaml:"result_body"`   // ResultBody selects the source of the body shown for the tool result.
}

// yamlPresenterSummaryItemSpec describes one YAML-configured presenter summary segment.
type yamlPresenterSummaryItemSpec struct {
	Text  string `yaml:"text"`  // Text is literal summary text.
	Param string `yaml:"param"` // Param names a tool parameter whose value is shown in the summary.
}

// yamlNormalizedPresenterSpec selects the validated presenter preset for a YAML-defined tool.
type yamlNormalizedPresenterSpec struct {
	SubagentQA *yamlNormalizedSubagentQAPresenterSpec // SubagentQA configures Q-and-A style subagent presentation.
	Review     *yamlNormalizedReviewPresenterSpec     // Review configures structured review presentation.
}

// yamlNormalizedSubagentQAPresenterSpec configures the validated subagent Q-and-A presenter preset.
type yamlNormalizedSubagentQAPresenterSpec struct {
	CallAction   string                               // CallAction is the action text shown while the subagent call is in progress.
	ResultAction string                               // ResultAction is the action text shown after the subagent returns.
	SummaryItems []yamlNormalizedPresenterSummaryItem // SummaryItems are ordered literal and parameter segments appended to the summary.
	CallBody     string                               // CallBody names the parameter shown as the call body, or suppresses the call body.
	ResultBody   string                               // ResultBody selects whether the tool result is shown as the result body.
}

// yamlNormalizedPresenterSummaryItem describes one validated presenter summary segment.
type yamlNormalizedPresenterSummaryItem struct {
	Text  string // Text is literal summary text.
	Param string // Param names the tool parameter whose value is shown in the summary.
}

// yamlNormalizedReviewPresenterSpec marks a validated review presenter preset.
type yamlNormalizedReviewPresenterSpec struct{}

// A yamlSubagentQAPresenter renders YAML subagent tools as Q-and-A tool call and result presentations.
type yamlSubagentQAPresenter struct {
	paramSpecs map[string]yamlNormalizedParameter    // ParamSpecs are the normalized parameter definitions used to parse call input for presentation.
	spec       yamlNormalizedSubagentQAPresenterSpec // Spec is the validated Q-and-A presentation configuration.
}

// yamlReviewPresenter presents YAML-defined review tool calls and results.
type yamlReviewPresenter struct {
	paramSpecs map[string]yamlNormalizedParameter // ParamSpecs are the normalized parameter definitions used to parse call input for summaries.
}

var _ llmstream.Presenter = (*yamlSubagentQAPresenter)(nil)
var _ llmstream.Presenter = (*yamlReviewPresenter)(nil)
var _ llmstream.SubagentFinalMessagePresenter = (*yamlSubagentQAPresenter)(nil)
var _ llmstream.SubagentFinalMessagePresenter = (*yamlReviewPresenter)(nil)

// A yamlReviewResult is the structured JSON payload accepted by the review presenter.
type yamlReviewResult struct {
	Findings           *[]yamlReviewFinding `json:"findings"`            // Findings is the required list of actionable findings; an empty list means no findings.
	OverallCorrectness string               `json:"overall_correctness"` // OverallCorrectness is "patch is correct" or "patch is incorrect".
	OverallExplanation string               `json:"overall_explanation"` // OverallExplanation explains the overall correctness assessment.

	// OverallConfidenceScore is the optional 0-to-1 confidence score for the overall assessment.
	OverallConfidenceScore *float64 `json:"overall_confidence_score"`
}

// A yamlReviewFinding is one actionable finding in a structured review result.
type yamlReviewFinding struct {
	Title           string                  `json:"title"`              // Title is the concise human-readable finding summary.
	Body            string                  `json:"body"`               // Body explains the issue and recommended action.
	ConfidenceScore *float64                `json:"confidence_score"`   // ConfidenceScore is the optional 0-to-1 confidence score for the finding.
	Priority        *int                    `json:"priority,omitempty"` // Priority is the optional review priority in the inclusive range 0 through 3.
	CodeLocation    *yamlReviewCodeLocation `json:"code_location"`      // CodeLocation is the required file and line range for the finding.
}

// A yamlReviewCodeLocation identifies the file and lines for a review finding.
type yamlReviewCodeLocation struct {
	AbsoluteFilePath string               `json:"absolute_file_path"` // AbsoluteFilePath is the absolute path to the file containing the finding.
	LineRange        *yamlReviewLineRange `json:"line_range"`         // LineRange is the 1-based inclusive line range for the finding.
}

// yamlReviewLineRange describes a 1-based inclusive line range for a review finding.
type yamlReviewLineRange struct {
	Start *int `json:"start"` // Start is the first line in the range and must be positive.
	End   *int `json:"end"`   // End is the last line in the range and must be greater than or equal to Start.
}

// normalizeYAMLPresenterSpec validates an optional YAML tool presenter and returns its normalized preset configuration. A nil spec selects default presentation;
// non-nil specs must name a supported preset, and params are used to validate parameter references.
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

// normalizeYAMLSubagentQAPresenterSpec validates a subagent_q_and_a presenter preset. spec must be non-nil. The preset must provide call_action, result_action,
// call_body, and result_body; summary item and call_body parameter references must exist in params. call_body may be "-" to omit the call body, and result_body
// must be "result" or "-".
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

// normalizeYAMLReviewPresenterSpec validates the review presenter preset and returns its marker spec.
//
// The review preset requires a base parameter and does not support custom presenter actions, summary items, or bodies.
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

// Present implements llmstream.Presenter for YAML Q-and-A subagent tools.
//
// It returns an append presentation with a rendered summary and, when configured, a call or result body. When result is nil, it presents the in-progress call; otherwise,
// it presents the completed result. Invalid call parameters suppress parameter-derived text but do not make presentation fail.
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

// SubagentFinalMessage returns nil so the configured result body controls how the subagent answer is shown.
func (p *yamlSubagentQAPresenter) SubagentFinalMessage(llmstream.ToolCall, string, string) llmstream.Block {
	return nil
}

// summary returns the rendered Q-and-A presenter summary for a call or result.
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

// callBody returns the output body for an in-progress subagent call. It returns false when the presenter suppresses call bodies or the selected parameter has no
// displayable text.
func (p *yamlSubagentQAPresenter) callBody(params map[string]any) (llmstream.Output, bool) {
	if p.spec.CallBody == yamlPresenterBodyNone {
		return llmstream.Output{}, false
	}
	return yamlPresenterOutput(yamlPresenterParamText(params, p.spec.CallBody))
}

// The resultBody method returns the rendered result body for a Q-and-A subagent presentation.
//
// It returns false when the presenter is configured not to show result bodies or when result.Result is empty after trimming.
func (p *yamlSubagentQAPresenter) resultBody(result llmstream.ToolResult) (llmstream.Output, bool) {
	if p.spec.ResultBody != yamlPresenterBodyResult {
		return llmstream.Output{}, false
	}
	return yamlPresenterOutput(result.Result)
}

// Present returns the semantic presentation for a YAML review tool call or result. A nil result represents the in-progress call and produces only the review summary.
// When result is present, Present appends any displayable review body produced from result.Result while leaving tool errors to the shared default error renderer.
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

// SubagentFinalMessage returns nil so review output is presented only through the tool result.
func (p *yamlReviewPresenter) SubagentFinalMessage(llmstream.ToolCall, string, string) llmstream.Block {
	return nil
}

// The yamlReviewSummary function builds the one-line presentation summary for a review tool call or result.
//
// It uses "Reviewing" for calls, "Reviewed" for results, and appends the base parameter when present.
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

// The yamlReviewResultBody function converts a review tool result into displayable output.
//
// Valid structured review JSON is summarized as "No actionable findings." or as finding titles, capped at yamlReviewMaxFindings with a remaining-count line. Invalid
// JSON falls back to trimmed verbatim output. The bool reports whether any output should be shown.
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

// validateYAMLReviewResult reports whether result is a complete review payload accepted by the review presenter. It requires a findings list, a recognized overall
// correctness value, a non-empty explanation, a 0-to-1 confidence score, and valid findings.
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

// validateYAMLReviewFinding reports whether a structured review finding is complete and in range.
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
