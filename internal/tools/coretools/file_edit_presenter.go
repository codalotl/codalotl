package coretools

import (
	"encoding/json"
	"strings"

	"github.com/codalotl/codalotl/internal/llmstream"
)

var editPresenterInstance llmstream.Presenter = editPresenter{}
var writePresenterInstance llmstream.Presenter = writePresenter{}

// The editPresenter type implements llmstream.Presenter for the edit tool. It presents file edits as semantic diffs and has a ready-to-use zero value.
type editPresenter struct{}

// Present returns the semantic diff presentation for an edit tool call. If the call input cannot be converted to a diff, it returns an "Edit" fallback summary.
func (p editPresenter) Present(call llmstream.ToolCall, result *llmstream.ToolResult) llmstream.Presentation {
	diff, ok := editPresenterDiff(call)
	return fileEditPresenterPresentation(call, result, diff, ok, "Edit")
}

// The writePresenter type implements llmstream.Presenter for the write tool. It presents file writes as semantic diffs and has a ready-to-use zero value.
type writePresenter struct{}

// Present returns the semantic diff presentation for a write tool call. If the call input cannot be converted to a diff, it returns a "Write" fallback summary.
func (p writePresenter) Present(call llmstream.ToolCall, result *llmstream.ToolResult) llmstream.Presentation {
	diff, ok := writePresenterDiff(call)
	return fileEditPresenterPresentation(call, result, diff, ok, "Write")
}

// The fileEditPresenterPresentation function builds the shared presentation for edit-like file tools. It returns a fallback action summary when no diff can be built;
// otherwise it uses the diff body and attaches tool errors to the last edit.
func fileEditPresenterPresentation(call llmstream.ToolCall, result *llmstream.ToolResult, diff llmstream.Diff, ok bool, fallbackAction string) llmstream.Presentation {
	presentation := llmstream.Presentation{
		Behavior: llmstream.CompletionBehaviorReplace,
	}

	if !ok {
		presentation.Summary = llmstream.Line{
			Segments: []llmstream.Segment{
				{Text: fileEditPresenterAction(call, fallbackAction), Role: llmstream.RoleAction},
			},
		}
		return presentation
	}

	if result != nil && result.IsError {
		presentation.ErrorBehavior = llmstream.ErrorBehaviorPresenterOwned
		diff = fileEditPresenterAttachError(diff, strings.TrimSpace(result.Result))
	}

	presentation.Body = diff
	return presentation
}

func fileEditPresenterAction(call llmstream.ToolCall, fallback string) string {
	name := strings.TrimSpace(call.Name)
	if name != "" {
		return name
	}
	return fallback
}

func fileEditPresenterAttachError(diff llmstream.Diff, message string) llmstream.Diff {
	if len(diff.Edits) == 0 || message == "" {
		return diff
	}

	edit := &diff.Edits[len(diff.Edits)-1]
	edit.Error = &llmstream.Line{
		Segments: []llmstream.Segment{
			{Text: "Error: " + message, Role: llmstream.RoleError},
		},
	}
	return diff
}

// The editPresenterParams type is the JSON input shape used to build an edit diff presentation.
type editPresenterParams struct {
	Path       string `json:"path"`        // Path is the primary file path parameter.
	FilePath   string `json:"file_path"`   // FilePath is an alternate file path parameter accepted for compatibility.
	OldString  string `json:"old_string"`  // OldString is the first source-text alias used for deleted diff lines.
	OldText    string `json:"old_text"`    // OldText is the second source-text alias used for deleted diff lines.
	Find       string `json:"find"`        // Find is the third source-text alias used for deleted diff lines.
	NewString  string `json:"new_string"`  // NewString is the first replacement-text alias used for added diff lines.
	NewText    string `json:"new_text"`    // NewText is the second replacement-text alias used for added diff lines.
	Replace    string `json:"replace"`     // Replace is the third replacement-text alias used for added diff lines.
	ReplaceAll bool   `json:"replace_all"` // ReplaceAll reports whether the diff represents replacing every match.
}

// editPresenterDiff builds a semantic edit diff from an edit tool call. It returns false when the call input is not valid JSON or does not include a path; otherwise
// it includes the matched text as deleted lines, the replacement text as added lines, and preserves replace_all.
func editPresenterDiff(call llmstream.ToolCall) (llmstream.Diff, bool) {
	var params editPresenterParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return llmstream.Diff{}, false
	}

	path := fileEditPresenterFirstPath(params.Path, params.FilePath)
	if path == "" {
		return llmstream.Diff{}, false
	}

	oldText := fileEditPresenterFirstNonEmpty(params.OldString, params.OldText, params.Find)
	newText := fileEditPresenterFirstNonEmpty(params.NewString, params.NewText, params.Replace)

	diffLines := make([]llmstream.DiffLine, 0)
	for _, line := range fileEditPresenterLines(oldText) {
		diffLines = append(diffLines, llmstream.DiffLine{
			Kind: llmstream.DiffLineKindDelete,
			Text: line,
		})
	}
	for _, line := range fileEditPresenterLines(newText) {
		diffLines = append(diffLines, llmstream.DiffLine{
			Kind: llmstream.DiffLineKindAdd,
			Text: line,
		})
	}

	return llmstream.Diff{
		Edits: []llmstream.DiffEdit{{
			Kind:       llmstream.DiffEditKindEdit,
			OldPath:    path,
			ReplaceAll: params.ReplaceAll,
			Lines:      diffLines,
		}},
	}, true
}

// The writePresenterParams type is the JSON input shape used to build a write diff presentation.
type writePresenterParams struct {
	Path     string `json:"path"`      // Path is the primary file path parameter.
	FilePath string `json:"file_path"` // FilePath is an alternate file path parameter accepted for compatibility.
	Content  string `json:"content"`   // Content is the primary file content parameter.
	Contents string `json:"contents"`  // Contents is an alternate file content parameter accepted for compatibility.
	Text     string `json:"text"`      // Text is an alternate file content parameter accepted for compatibility.
	FileText string `json:"file_text"` // FileText is an alternate file content parameter accepted for compatibility.
}

// The writePresenterDiff function builds the semantic diff shown for a write tool call. It accepts compatible path and content parameter names and returns ok false
// when the call input is invalid or has no path.
func writePresenterDiff(call llmstream.ToolCall) (llmstream.Diff, bool) {
	var params writePresenterParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return llmstream.Diff{}, false
	}

	path := fileEditPresenterFirstPath(params.Path, params.FilePath)
	if path == "" {
		return llmstream.Diff{}, false
	}

	content := fileEditPresenterFirstNonEmpty(params.Content, params.Contents, params.Text, params.FileText)
	diffLines := make([]llmstream.DiffLine, 0)
	for _, line := range fileEditPresenterLines(content) {
		diffLines = append(diffLines, llmstream.DiffLine{
			Kind: llmstream.DiffLineKindAdd,
			Text: line,
		})
	}

	return llmstream.Diff{
		Edits: []llmstream.DiffEdit{{
			Kind:    llmstream.DiffEditKindAdd,
			NewPath: path,
			Lines:   diffLines,
		}},
	}, true
}

func fileEditPresenterLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func fileEditPresenterFirstPath(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func fileEditPresenterFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
