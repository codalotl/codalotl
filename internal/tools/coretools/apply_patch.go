package coretools

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/codalotl/codalotl/internal/applypatch"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
)

//go:embed apply_patch_freeform.md
var descriptionApplyPatchFreeform string

//go:embed apply_patch_function.md
var descriptionApplyPatchFunction string

const ToolNameApplyPatch = "apply_patch"

func NewApplyPatchTool(authorizer authdomain.Authorizer, useFreeformTool bool, postChecks *ApplyPatchPostChecks) llmstream.Tool {
	sandboxAbsDir := authorizer.SandboxDir()
	return &toolApplyPatch{
		sandboxAbsDir: sandboxAbsDir,
		useFreeform:   useFreeformTool,
		authorizer:    authorizer,
		postChecks:    postChecks,
	}
}

type toolApplyPatch struct {
	sandboxAbsDir string
	useFreeform   bool
	authorizer    authdomain.Authorizer
	postChecks    *ApplyPatchPostChecks
}

func (t *toolApplyPatch) Name() string {
	return ToolNameApplyPatch
}

func (t *toolApplyPatch) Presenter() llmstream.Presenter {
	return applyPatchPresenterInstance
}

func (t *toolApplyPatch) Info() llmstream.ToolInfo {
	if t.useFreeform {
		// NOTE: custom tools don't currently support request_permission...
		return llmstream.ToolInfo{
			Name:        ToolNameApplyPatch,
			Description: strings.TrimSpace(descriptionApplyPatchFreeform),
			Kind:        llmstream.ToolKindCustom,
			Grammar: &llmstream.ToolGrammar{
				Syntax:     llmstream.ToolGrammarSyntaxLark,
				Definition: applypatch.ApplyPatchGrammar,
			},
		}
	}
	return llmstream.ToolInfo{
		Name:        ToolNameApplyPatch,
		Description: strings.TrimSpace(descriptionApplyPatchFunction),
		Parameters: map[string]any{
			"patch": map[string]any{
				"type":        "string",
				"description": "Patch to apply using the ApplyPatch grammar",
			},
			"request_permission": map[string]any{
				"type":        "boolean",
				"description": "Optionally request the user's permission to apply this patch. Set to true for material access outside sandbox dir",
			},
		},
		Required: []string{"patch"},
		Kind:     llmstream.ToolKindFunction,
	}
}

func (t *toolApplyPatch) Run(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
	patch, requestPermission, err := t.extractPatch(call)
	if err != nil {
		return NewToolErrorResult(call, err.Error(), err)
	}

	paths, err := t.collectPatchPaths(patch)
	if err != nil {
		return NewToolErrorResult(call, err.Error(), err)
	}

	if t.authorizer != nil && len(paths) > 0 {
		if authErr := t.authorizer.IsAuthorizedForWrite(requestPermission, "", ToolNameApplyPatch, paths...); authErr != nil {
			return NewToolErrorResult(call, authErr.Error(), authErr)
		}
	}

	fileChanges, err := applypatch.ApplyPatch(t.sandboxAbsDir, patch)
	if err != nil {
		return NewToolErrorResult(call, err.Error(), err)
	}

	result := buildApplyPatchSuccessPayload(fileChanges)

	if t.shouldRunPostChecks() {
		extraOutputs, err := t.runPostApplyChecks(ctx, fileChanges)
		if err != nil {
			result = result + "\n\nPost apply_patch checks errored: " + err.Error()
		} else if len(extraOutputs) > 0 {
			result = result + "\n" + strings.Join(extraOutputs, "\n")
		}
	}

	return llmstream.ToolResult{
		CallID: call.CallID,
		Name:   call.Name,
		Type:   call.Type,
		Result: result,
	}
}

type applyPatchFunctionParams struct {
	Patch             string `json:"patch"`
	RequestPermission bool   `json:"request_permission"`
}

func (t *toolApplyPatch) shouldRunPostChecks() bool {
	return shouldRunPostChecks(t.postChecks)
}

func (t *toolApplyPatch) extractPatch(call llmstream.ToolCall) (string, bool, error) {
	if t.useFreeform {
		if strings.TrimSpace(call.Input) == "" {
			return "", false, fmt.Errorf("patch input is required")
		}
		return call.Input, false, nil
	}

	var params applyPatchFunctionParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return "", false, fmt.Errorf("error parsing parameters: %s", err)
	}

	if strings.TrimSpace(params.Patch) == "" {
		return "", params.RequestPermission, fmt.Errorf("patch input is required")
	}

	return params.Patch, params.RequestPermission, nil
}

func buildApplyPatchSuccessPayload(changes []applypatch.FileChange) string {
	lines := make([]string, 0, len(changes)+2)
	lines = append(lines, "Updated the following files:")
	for _, change := range changes {
		lines = append(lines, fmt.Sprintf("%s %s", formatFileChangeKind(change.Kind), change.Path))
	}
	content := strings.Join(lines, "\n")
	return fmt.Sprintf("<apply-patch ok=\"true\">\n%s\n</apply-patch>", content)
}

func formatFileChangeKind(kind applypatch.FileChangeKind) string {
	switch kind {
	case applypatch.FileChangeAdded:
		return "A"
	case applypatch.FileChangeModified:
		return "M"
	case applypatch.FileChangeDeleted:
		return "D"
	default:
		return "?"
	}
}

func (t *toolApplyPatch) collectPatchPaths(patch string) ([]string, error) {
	scanner := bufio.NewScanner(strings.NewReader(patch))
	paths := make(map[string]struct{})
	result := make([]string, 0, 8)
	for scanner.Scan() {
		line := scanner.Text()
		var raw string
		switch {
		case strings.HasPrefix(line, "*** Add File: "):
			raw = strings.TrimSpace(strings.TrimPrefix(line, "*** Add File: "))
		case strings.HasPrefix(line, "*** Delete File: "):
			raw = strings.TrimSpace(strings.TrimPrefix(line, "*** Delete File: "))
		case strings.HasPrefix(line, "*** Update File: "):
			raw = strings.TrimSpace(strings.TrimPrefix(line, "*** Update File: "))
		case strings.HasPrefix(line, "*** Move to: "):
			raw = strings.TrimSpace(strings.TrimPrefix(line, "*** Move to: "))
		default:
			continue
		}
		if raw == "" {
			return nil, fmt.Errorf("path is required")
		}
		abs, err := t.resolvePatchPath(raw)
		if err != nil {
			return nil, err
		}
		if _, exists := paths[abs]; exists {
			continue
		}
		paths[abs] = struct{}{}
		result = append(result, abs)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (t *toolApplyPatch) resolvePatchPath(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", fmt.Errorf("path is required")
	}

	path := filepath.FromSlash(raw)
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	return filepath.Join(t.sandboxAbsDir, path), nil
}

func (t *toolApplyPatch) runPostApplyChecks(ctx context.Context, changes []applypatch.FileChange) ([]string, error) {
	changedPaths := make([]string, 0, len(changes))
	for _, change := range changes {
		changedPaths = append(changedPaths, change.Path)
	}
	return runPostChecks(ctx, t.sandboxAbsDir, t.postChecks, changedPaths)
}

var applyPatchPresenterInstance llmstream.Presenter = applyPatchPresenter{}

type applyPatchPresenter struct{}

func (p applyPatchPresenter) Present(call llmstream.ToolCall, result *llmstream.ToolResult) llmstream.Presentation {
	diff, _ := applyPatchPresenterDiff(call)
	presentation := llmstream.Presentation{
		Behavior:      llmstream.CompletionBehaviorReplace,
		ErrorBehavior: llmstream.ErrorBehaviorDefault,
	}

	if len(diff.Edits) > 0 {
		presentation.Body = diff
	} else {
		presentation.Summary = applyPatchPresenterSummary(diff)
	}

	if !applyPatchPresenterFailed(result) {
		return presentation
	}

	presentation.ErrorBehavior = llmstream.ErrorBehaviorPresenterOwned

	if result != nil && applypatch.IsInvalidPatch(result.SourceErr) {
		if bestEffortDiff, ok := applyPatchPresenterBestEffortDiff(call); ok {
			presentation.Summary = llmstream.Line{}
			if len(bestEffortDiff.Edits) > 0 {
				presentation.Body = bestEffortDiff
			}
		}
	}

	if line, ok := applyPatchPresenterErrorLine(result); ok && presentation.Body != nil {
		if diff, ok := presentation.Body.(llmstream.Diff); ok && len(diff.Edits) > 0 {
			diff.Edits[len(diff.Edits)-1].Error = &line
			presentation.Body = diff
		}
	}

	return presentation
}

func applyPatchPresenterDiff(call llmstream.ToolCall) (llmstream.Diff, bool) {
	source, ok := applyPatchPresenterSource(call)
	if !ok {
		return llmstream.Diff{}, false
	}

	edits, err := parseApplyPatchPresenterEdits(source)
	if err != nil || len(edits) == 0 {
		return llmstream.Diff{}, false
	}

	return llmstream.Diff{
		Edits: edits,
	}, true
}

func applyPatchPresenterSource(call llmstream.ToolCall) (string, bool) {
	input := strings.TrimSpace(call.Input)
	if input == "" {
		return "", false
	}

	var params applyPatchFunctionParams
	if err := json.Unmarshal([]byte(input), &params); err == nil {
		if patch := strings.TrimSpace(params.Patch); patch != "" {
			return patch, true
		}
	}

	return input, true
}

func parseApplyPatchPresenterEdits(input string) ([]llmstream.DiffEdit, error) {
	normalized := strings.ReplaceAll(input, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")

	i := 0
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i >= len(lines) || strings.TrimSpace(lines[i]) != "*** Begin Patch" {
		return nil, fmt.Errorf("missing begin marker")
	}
	i++

	var edits []llmstream.DiffEdit
	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			i++
			continue
		}
		if trimmed == "*** End Patch" {
			break
		}

		switch {
		case strings.HasPrefix(line, "*** Add File: "):
			path := strings.TrimSpace(strings.TrimPrefix(line, "*** Add File: "))
			i++
			var diffLines []llmstream.DiffLine
		forAdd:
			for i < len(lines) {
				cur := lines[i]
				if strings.HasPrefix(cur, "***") {
					break
				}
				switch {
				case cur == "":
					diffLines = append(diffLines, llmstream.DiffLine{Kind: llmstream.DiffLineKindContext, Text: ""})
					i++
				case cur[0] == '+':
					diffLines = append(diffLines, llmstream.DiffLine{Kind: llmstream.DiffLineKindAdd, Text: applyPatchPresenterNormalizeDiffText(cur[1:])})
					i++
				default:
					break forAdd
				}
			}
			edits = append(edits, llmstream.DiffEdit{
				Kind:    llmstream.DiffEditKindAdd,
				NewPath: path,
				Lines:   cleanApplyPatchPresenterLines(diffLines),
			})
		case strings.HasPrefix(line, "*** Delete File: "):
			path := strings.TrimSpace(strings.TrimPrefix(line, "*** Delete File: "))
			edits = append(edits, llmstream.DiffEdit{
				Kind:    llmstream.DiffEditKindDelete,
				OldPath: path,
			})
			i++
		case strings.HasPrefix(line, "*** Update File: "):
			path := strings.TrimSpace(strings.TrimPrefix(line, "*** Update File: "))
			i++

			moveTo := ""
			if i < len(lines) && strings.HasPrefix(lines[i], "*** Move to: ") {
				moveTo = strings.TrimSpace(strings.TrimPrefix(lines[i], "*** Move to: "))
				i++
			}

			var diffLines []llmstream.DiffLine
			havePrintedLines := false
			for i < len(lines) {
				cur := lines[i]
				switch {
				case strings.HasPrefix(cur, "***"):
					goto finishUpdate
				case strings.HasPrefix(cur, "@@"):
					if havePrintedLines && !lastApplyPatchPresenterLineIsOmitted(diffLines) {
						diffLines = append(diffLines, llmstream.DiffLine{Kind: llmstream.DiffLineKindOmitted})
					}
					i++
				case strings.TrimSpace(cur) == "*** End of File":
					i++
				case cur == "":
					diffLines = append(diffLines, llmstream.DiffLine{Kind: llmstream.DiffLineKindContext, Text: ""})
					havePrintedLines = true
					i++
				default:
					switch cur[0] {
					case '+':
						diffLines = append(diffLines, llmstream.DiffLine{Kind: llmstream.DiffLineKindAdd, Text: applyPatchPresenterNormalizeDiffText(cur[1:])})
					case '-':
						diffLines = append(diffLines, llmstream.DiffLine{Kind: llmstream.DiffLineKindDelete, Text: applyPatchPresenterNormalizeDiffText(cur[1:])})
					case ' ':
						diffLines = append(diffLines, llmstream.DiffLine{Kind: llmstream.DiffLineKindContext, Text: applyPatchPresenterNormalizeDiffText(cur[1:])})
					default:
						diffLines = append(diffLines, llmstream.DiffLine{Kind: llmstream.DiffLineKindContext, Text: cur})
					}
					havePrintedLines = true
					i++
				}
			}

		finishUpdate:
			edit := llmstream.DiffEdit{
				Kind:    llmstream.DiffEditKindEdit,
				OldPath: path,
				Lines:   cleanApplyPatchPresenterLines(diffLines),
			}
			if moveTo != "" {
				edit.Kind = llmstream.DiffEditKindRename
				edit.NewPath = moveTo
			}
			edits = append(edits, edit)
		default:
			i++
		}
	}

	if len(edits) == 0 {
		return nil, fmt.Errorf("no patch hunks")
	}

	return edits, nil
}

func applyPatchPresenterBestEffortDiff(call llmstream.ToolCall) (llmstream.Diff, bool) {
	source, ok := applyPatchPresenterSource(call)
	if !ok {
		return llmstream.Diff{}, false
	}

	source = strings.ReplaceAll(source, "\r\n", "\n")
	lines := strings.Split(source, "\n")

	edits := make([]llmstream.DiffEdit, 0, 4)
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		switch {
		case strings.HasPrefix(line, "*** Add File: "):
			path := strings.TrimSpace(strings.TrimPrefix(line, "*** Add File: "))
			if path == "" {
				continue
			}
			edits = append(edits, llmstream.DiffEdit{
				Kind:    llmstream.DiffEditKindAdd,
				NewPath: path,
			})
		case strings.HasPrefix(line, "*** Delete File: "):
			path := strings.TrimSpace(strings.TrimPrefix(line, "*** Delete File: "))
			if path == "" {
				continue
			}
			edits = append(edits, llmstream.DiffEdit{
				Kind:    llmstream.DiffEditKindDelete,
				OldPath: path,
			})
		case strings.HasPrefix(line, "*** Update File: "):
			path := strings.TrimSpace(strings.TrimPrefix(line, "*** Update File: "))
			if path == "" {
				continue
			}
			edit := llmstream.DiffEdit{
				Kind:    llmstream.DiffEditKindEdit,
				OldPath: path,
			}
			if i+1 < len(lines) && strings.HasPrefix(lines[i+1], "*** Move to: ") {
				toPath := strings.TrimSpace(strings.TrimPrefix(lines[i+1], "*** Move to: "))
				if toPath != "" {
					edit.Kind = llmstream.DiffEditKindRename
					edit.NewPath = toPath
				}
			}
			edits = append(edits, edit)
		}
	}

	if len(edits) == 0 {
		return llmstream.Diff{}, false
	}

	return llmstream.Diff{
		Edits: edits,
	}, true
}

func applyPatchPresenterSummary(diff llmstream.Diff) llmstream.Line {
	if len(diff.Edits) == 0 {
		return llmstream.Line{
			Segments: []llmstream.Segment{
				{Text: "Apply Patch", Role: llmstream.RoleAction},
			},
		}
	}

	edit := diff.Edits[0]
	path := applyPatchPresenterFirstNonEmpty(edit.OldPath, edit.NewPath)
	toPath := applyPatchPresenterFirstNonEmpty(edit.NewPath, edit.OldPath)

	switch edit.Kind {
	case llmstream.DiffEditKindAdd:
		return llmstream.Line{
			Segments: []llmstream.Segment{
				{Text: "Add", Role: llmstream.RoleAction},
				{Text: " " + applyPatchPresenterFirstNonEmpty(edit.NewPath, edit.OldPath), Role: llmstream.RoleNormal},
			},
		}
	case llmstream.DiffEditKindDelete:
		return llmstream.Line{
			Segments: []llmstream.Segment{
				{Text: "Delete", Role: llmstream.RoleAction},
				{Text: " " + applyPatchPresenterFirstNonEmpty(edit.OldPath, edit.NewPath), Role: llmstream.RoleNormal},
			},
		}
	case llmstream.DiffEditKindRename:
		action := "Edit"
		if len(edit.Lines) == 0 {
			action = "Rename"
		}
		return llmstream.Line{
			Segments: []llmstream.Segment{
				{Text: action, Role: llmstream.RoleAction},
				{Text: " " + path, Role: llmstream.RoleNormal},
				{Text: " → " + toPath, Role: llmstream.RoleAccent},
			},
		}
	default:
		summary := llmstream.Line{
			Segments: []llmstream.Segment{
				{Text: "Edit", Role: llmstream.RoleAction},
				{Text: " " + path, Role: llmstream.RoleNormal},
			},
		}
		if edit.NewPath != "" && edit.NewPath != edit.OldPath {
			summary.Segments = append(summary.Segments, llmstream.Segment{
				Text: " → " + edit.NewPath,
				Role: llmstream.RoleAccent,
			})
		}
		return summary
	}
}

func applyPatchPresenterFailed(result *llmstream.ToolResult) bool {
	if result == nil {
		return false
	}
	if result.IsError {
		return true
	}

	trimmed := strings.TrimSpace(result.Result)
	if trimmed == "" {
		return false
	}

	var payload struct {
		Success *bool  `json:"success"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return false
	}
	if payload.Success != nil {
		return !*payload.Success
	}
	return strings.TrimSpace(payload.Error) != ""
}

func applyPatchPresenterErrorLine(result *llmstream.ToolResult) (llmstream.Line, bool) {
	if result == nil {
		return llmstream.Line{}, false
	}
	if applypatch.IsInvalidPatch(result.SourceErr) {
		return llmstream.Line{
			Segments: []llmstream.Segment{
				{Text: "Failed: LLM supplied an invalid patch.", Role: llmstream.RoleAccent},
			},
		}, true
	}

	trimmed := strings.TrimSpace(result.Result)
	if trimmed == "" {
		return llmstream.Line{}, false
	}

	var payload struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(trimmed), &payload); err == nil && strings.TrimSpace(payload.Error) != "" {
		return llmstream.Line{
			Segments: []llmstream.Segment{
				{Text: "Error: " + payload.Error, Role: llmstream.RoleError},
			},
		}, true
	}

	if result.IsError {
		return llmstream.Line{
			Segments: []llmstream.Segment{
				{Text: "Error: " + trimmed, Role: llmstream.RoleError},
			},
		}, true
	}

	return llmstream.Line{}, false
}

func applyPatchPresenterFirstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func applyPatchPresenterNormalizeDiffText(text string) string {
	if strings.HasPrefix(text, " ") {
		return text[1:]
	}
	return text
}

func cleanApplyPatchPresenterLines(lines []llmstream.DiffLine) []llmstream.DiffLine {
	if len(lines) == 0 {
		return nil
	}

	cleaned := make([]llmstream.DiffLine, 0, len(lines))
	for _, line := range lines {
		if line.Kind == llmstream.DiffLineKindOmitted && lastApplyPatchPresenterLineIsOmitted(cleaned) {
			continue
		}
		cleaned = append(cleaned, line)
	}

	for len(cleaned) > 0 && cleaned[0].Kind == llmstream.DiffLineKindOmitted {
		cleaned = cleaned[1:]
	}
	for len(cleaned) > 0 && cleaned[len(cleaned)-1].Kind == llmstream.DiffLineKindOmitted {
		cleaned = cleaned[:len(cleaned)-1]
	}

	return cleaned
}

func lastApplyPatchPresenterLineIsOmitted(lines []llmstream.DiffLine) bool {
	return len(lines) > 0 && lines[len(lines)-1].Kind == llmstream.DiffLineKindOmitted
}
