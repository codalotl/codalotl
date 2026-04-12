package coretools

import (
	"encoding/json"
	"strings"

	"github.com/codalotl/codalotl/internal/llmstream"
)

var editPresenterInstance llmstream.Presenter = editPresenter{}
var writePresenterInstance llmstream.Presenter = writePresenter{}

type editPresenter struct{}

func (p editPresenter) Present(call llmstream.ToolCall, result *llmstream.ToolResult) llmstream.Presentation {
	diff, ok := editPresenterDiff(call)
	return fileEditPresenterPresentation(call, result, diff, ok, "Edit")
}

type writePresenter struct{}

func (p writePresenter) Present(call llmstream.ToolCall, result *llmstream.ToolResult) llmstream.Presentation {
	diff, ok := writePresenterDiff(call)
	return fileEditPresenterPresentation(call, result, diff, ok, "Write")
}

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

type editPresenterParams struct {
	Path       string `json:"path"`
	FilePath   string `json:"file_path"`
	OldString  string `json:"old_string"`
	OldText    string `json:"old_text"`
	Find       string `json:"find"`
	NewString  string `json:"new_string"`
	NewText    string `json:"new_text"`
	Replace    string `json:"replace"`
	ReplaceAll bool   `json:"replace_all"`
}

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

type writePresenterParams struct {
	Path     string `json:"path"`
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
	Contents string `json:"contents"`
	Text     string `json:"text"`
	FileText string `json:"file_text"`
}

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
