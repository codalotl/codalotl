package coretools

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"github.com/codalotl/codalotl/internal/applypatch"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"path/filepath"
	"strings"
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
	_ = result

	presentation := llmstream.Presentation{
		Behavior: llmstream.CompletionBehaviorReplace,
		Summary: llmstream.Line{
			Segments: []llmstream.Segment{
				{Text: "Apply Patch", Role: llmstream.RoleAction},
			},
		},
	}

	if diff, ok := applyPatchPresenterDiff(call); ok {
		presentation.Body = []llmstream.Block{diff}
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

	return llmstream.Diff{Edits: edits}, true
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
					diffLines = append(diffLines, llmstream.DiffLine{Kind: llmstream.DiffLineKindAdd, Text: cur[1:]})
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
						diffLines = append(diffLines, llmstream.DiffLine{Kind: llmstream.DiffLineKindAdd, Text: cur[1:]})
					case '-':
						diffLines = append(diffLines, llmstream.DiffLine{Kind: llmstream.DiffLineKindDelete, Text: cur[1:]})
					case ' ':
						diffLines = append(diffLines, llmstream.DiffLine{Kind: llmstream.DiffLineKindContext, Text: cur[1:]})
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
