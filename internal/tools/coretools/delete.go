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

const ToolNameDelete = "delete"

type toolDelete struct {
	sandboxAbsDir string
	authorizer    authdomain.Authorizer
}
type ParamsDelete struct {
	Path              string `json:"path"`
	RequestPermission bool   `json:"request_permission"`
}

func NewDeleteTool(authorizer authdomain.Authorizer) llmstream.Tool {
	sandboxAbsDir := authorizer.SandboxDir()
	return &toolDelete{
		sandboxAbsDir: sandboxAbsDir,
		authorizer:    authorizer,
	}
}
func (t *toolDelete) Name() string {
	return ToolNameDelete
}
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
