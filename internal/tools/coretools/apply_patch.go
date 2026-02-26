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
