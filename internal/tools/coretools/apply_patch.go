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

// ToolNameApplyPatch is the registered name of the apply_patch tool.
const ToolNameApplyPatch = "apply_patch"

// NewApplyPatchTool returns an apply_patch tool that authorizes patch target writes with authorizer and resolves paths relative to authorizer's sandbox. When useFreeformTool
// is true, the tool accepts freeform ApplyPatch input instead of JSON function parameters. If postChecks is non-nil, it runs post-change checks after a successful
// patch.
func NewApplyPatchTool(authorizer authdomain.Authorizer, useFreeformTool bool, postChecks *ApplyPatchPostChecks) llmstream.Tool {
	sandboxAbsDir := authorizer.SandboxDir()
	return &toolApplyPatch{
		sandboxAbsDir: sandboxAbsDir,
		useFreeform:   useFreeformTool,
		authorizer:    authorizer,
		postChecks:    postChecks,
	}
}

// The toolApplyPatch type implements the apply_patch tool by authorizing patch targets, applying ApplyPatch edits, and optionally running post-change checks.
type toolApplyPatch struct {
	sandboxAbsDir string                // This is the absolute sandbox root for path resolution and post-checks.
	useFreeform   bool                  // This selects custom freeform ApplyPatch input instead of JSON function parameters.
	authorizer    authdomain.Authorizer // This authorizes writes to patch target paths before the patch is applied.
	postChecks    *ApplyPatchPostChecks // This configures optional diagnostics and lint hooks after a successful patch.
}

// Name returns the registered name of the apply_patch tool.
func (t *toolApplyPatch) Name() string {
	return ToolNameApplyPatch
}

// Presenter returns the presenter used to display apply_patch calls as semantic diffs.
func (t *toolApplyPatch) Presenter() llmstream.Presenter {
	return applyPatchPresenterInstance
}

// Info returns the tool metadata for apply_patch. It exposes a grammar-based custom tool when freeform input is enabled; otherwise it exposes a JSON function tool
// with a required patch parameter and optional request_permission.
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

// Run executes an apply_patch tool call by extracting the patch, authorizing each affected path, and applying it in the sandbox. On success it returns the changed
// files and appends configured post-check output; patch parsing, authorization, and apply failures are returned as tool errors.
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

// applyPatchFunctionParams contains the JSON arguments for structured apply_patch calls.
type applyPatchFunctionParams struct {
	Patch             string `json:"patch"`              // Patch is the patch text to apply and must not be blank.
	RequestPermission bool   `json:"request_permission"` // RequestPermission asks for approval to apply the patch when policy requires it.
}

// The shouldRunPostChecks method reports whether the apply_patch tool has any post-change hooks configured.
func (t *toolApplyPatch) shouldRunPostChecks() bool {
	return shouldRunPostChecks(t.postChecks)
}

// The extractPatch method parses an apply_patch call and returns the patch text and request-permission flag. In freeform mode, call.Input is used directly and request
// permission is false. In structured mode, call.Input must be JSON containing a nonblank patch.
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

// The collectPatchPaths method returns the unique absolute paths named by an ApplyPatch document in first-seen order. It recognizes add, delete, update, and move-to
// headers and returns an error for missing paths or scan failures.
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

// The resolvePatchPath method converts an ApplyPatch path to an absolute filesystem path. Empty paths are rejected; absolute paths are cleaned, and relative slash-separated
// paths are resolved from the sandbox root.
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

// The runPostApplyChecks method runs configured post-change checks for files changed by apply_patch. It passes changed file paths to the shared post-check runner
// and returns any check output or error.
func (t *toolApplyPatch) runPostApplyChecks(ctx context.Context, changes []applypatch.FileChange) ([]string, error) {
	changedPaths := make([]string, 0, len(changes))
	for _, change := range changes {
		changedPaths = append(changedPaths, change.Path)
	}
	return runPostChecks(ctx, t.sandboxAbsDir, t.postChecks, changedPaths)
}

var applyPatchPresenterInstance llmstream.Presenter = applyPatchPresenter{}

// An applyPatchPresenter presents apply_patch tool calls as semantic diffs and attaches patch errors when possible.
type applyPatchPresenter struct{}

// Present returns the semantic presentation for an apply_patch call, using result when available to model errors. It renders parseable patches as diff bodies, falls
// back to a one-line summary when no diff is available, and owns error rendering for failed patch results so invalid patches can show best-effort edits with an
// attached error.
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

// parseApplyPatchPresenterEdits parses ApplyPatch text into semantic diff edits for presentation. The input must begin with an ApplyPatch begin marker after any
// leading blank lines; the function returns an error when no recognizable file edits are present.
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

// applyPatchPresenterBestEffortDiff returns a file-level diff from an apply_patch call even when the patch cannot be fully parsed. It recognizes add, delete, update,
// and move headers and returns false when no target files can be identified.
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

// applyPatchPresenterSummary returns a one-line summary for the lead edit in diff. Empty diffs summarize as "Apply Patch"; adds, deletes, edits, and renames summarize
// with the corresponding action and path.
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

// The applyPatchPresenterFailed function reports whether result should be presented as a failed apply_patch call. It treats IsError as failed; otherwise, a JSON
// success field is authoritative, and a non-empty error field fails only when success is absent.
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

// applyPatchPresenterErrorLine returns a semantic error line for an apply_patch result, when one should be displayed. It recognizes invalid patch errors and JSON
// or raw tool error messages; if result is nil or has no displayable error, ok is false.
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

// The cleanApplyPatchPresenterLines function normalizes diff lines by collapsing repeated omitted markers and trimming omitted markers from the ends. It returns
// nil for empty input and preserves the order of all non-omitted lines.
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
