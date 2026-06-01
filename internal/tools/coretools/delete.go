package coretools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"os"
	"strings"
)

//go:embed delete.md
var descriptionDelete string

// ToolNameDelete is the registered name of the delete tool.
const ToolNameDelete = "delete"

// A toolDelete implements the delete tool for removing authorized files.
type toolDelete struct {
	sandboxAbsDir string                // This is the absolute sandbox root used to resolve relative delete paths.
	authorizer    authdomain.Authorizer // This authorizes delete requests before files are removed.
}

// ParamsDelete contains the JSON arguments for the delete tool.
type ParamsDelete struct {
	Path              string `json:"path"`               // Path is the file to delete. Relative paths are resolved from the sandbox root.
	RequestPermission bool   `json:"request_permission"` // RequestPermission asks for approval to delete the file when policy requires it.
}

// NewDeleteTool returns a delete tool that removes authorized files resolved relative to authorizer's sandbox. The authorizer must be non-nil.
func NewDeleteTool(authorizer authdomain.Authorizer) llmstream.Tool {
	sandboxAbsDir := authorizer.SandboxDir()
	return &toolDelete{
		sandboxAbsDir: sandboxAbsDir,
		authorizer:    authorizer,
	}
}

// Name returns ToolNameDelete, the registered name of the delete tool.
func (t *toolDelete) Name() string {
	return ToolNameDelete
}

// Presenter returns the semantic presenter for delete tool calls and results.
func (t *toolDelete) Presenter() llmstream.Presenter {
	return deletePresenterInstance
}

// Info returns the tool metadata for delete, including its embedded description and JSON parameters. The returned schema requires path and accepts request_permission
// for authorization escalation.
func (t *toolDelete) Info() llmstream.ToolInfo {
	return llmstream.ToolInfo{
		Name:        ToolNameDelete,
		Description: strings.TrimSpace(descriptionDelete),
		Parameters: map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "The path of the file to delete (absolute, or relative to sandbox dir)",
			},
			"request_permission": map[string]any{
				"type":        "boolean",
				"description": "Optionally request the user's permission to run this command. Set to true for material access outside sandbox dir",
			},
		},
		Required: []string{"path"},
	}
}

// Run executes a delete tool call by parsing its JSON parameters, validating and authorizing the target file, and removing it. It returns a tool error result for
// malformed input, missing or invalid paths, authorization failures, or filesystem removal errors.
func (t *toolDelete) Run(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
	_ = ctx
	var params ParamsDelete
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return NewToolErrorResult(call, fmt.Sprintf("error parsing parameters: %s", err), err)
	}
	if params.Path == "" {
		return llmstream.NewErrorToolResult("path is required", call)
	}
	absPath, relPath, normErr := NormalizePath(params.Path, t.sandboxAbsDir, WantPathTypeFile, true)
	if normErr != nil {
		return NewToolErrorResult(call, normErr.Error(), normErr)
	}
	if t.authorizer != nil {
		if authErr := t.authorizer.IsAuthorizedForWrite(params.RequestPermission, "", ToolNameDelete, absPath); authErr != nil {
			return NewToolErrorResult(call, authErr.Error(), authErr)
		}
	}
	if removeErr := os.Remove(absPath); removeErr != nil {
		return NewToolErrorResult(call, removeErr.Error(), removeErr)
	}
	displayPath := relPath
	if displayPath == "" {
		displayPath = absPath
	}
	return llmstream.ToolResult{
		CallID: call.CallID,
		Name:   call.Name,
		Type:   call.Type,
		Result: fmt.Sprintf("Deleted file: %s", displayPath),
	}
}

var deletePresenterInstance llmstream.Presenter = deletePresenter{}

// A deletePresenter presents delete tool calls as replacement summaries in the form "Delete <path>".
type deletePresenter struct{}

// Present returns a replacement presentation with the summary "Delete <path>" for a delete tool call. It uses the requested path when available and falls back to
// the call or tool name; result is ignored.
func (p deletePresenter) Present(call llmstream.ToolCall, result *llmstream.ToolResult) llmstream.Presentation {
	_ = result

	return llmstream.Presentation{
		Behavior: llmstream.CompletionBehaviorReplace,
		Summary: llmstream.Line{
			JoinWithSpace: true,
			Segments: []llmstream.Segment{
				{Text: "Delete", Role: llmstream.RoleAction},
				{Text: deletePresenterTarget(call), Role: llmstream.RoleNormal},
			},
		},
	}
}

func deletePresenterTarget(call llmstream.ToolCall) string {
	var params ParamsDelete
	if err := json.Unmarshal([]byte(call.Input), &params); err == nil {
		if path := strings.TrimSpace(params.Path); path != "" {
			return path
		}
	}

	name := strings.TrimSpace(call.Name)
	if name == "" {
		name = ToolNameDelete
	}
	return name
}
