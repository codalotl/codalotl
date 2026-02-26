package coretools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"os"
	"path/filepath"
	"strings"
)

//go:embed write.md
var descriptionWrite string

const ToolNameWrite = "write"

type toolWrite struct {
	sandboxAbsDir string
	authorizer    authdomain.Authorizer
}
type ParamsWrite struct {
	Path              string  `json:"path"`
	Content           *string `json:"content"`
	RequestPermission bool    `json:"request_permission"`
}

func NewWriteTool(authorizer authdomain.Authorizer) llmstream.Tool {
	sandboxAbsDir := authorizer.SandboxDir()
	return &toolWrite{
		sandboxAbsDir: sandboxAbsDir,
		authorizer:    authorizer,
	}
}
func (t *toolWrite) Name() string {
	return ToolNameWrite
}
func (t *toolWrite) Info() llmstream.ToolInfo {
	return llmstream.ToolInfo{
		Name:        ToolNameWrite,
		Description: strings.TrimSpace(descriptionWrite),
		Parameters: map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "The path of the file to write (absolute, or relative to sandbox dir)",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "The content to write to the file",
			},
			"request_permission": map[string]any{
				"type":        "boolean",
				"description": "Optionally request the user's permission to run this command. Set to true for material access outside sandbox dir",
			},
		},
		Required: []string{"path", "content"},
	}
}
func (t *toolWrite) Run(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
	_ = ctx
	var params ParamsWrite
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return NewToolErrorResult(call, fmt.Sprintf("error parsing parameters: %s", err), err)
	}
	if strings.TrimSpace(params.Path) == "" {
		return llmstream.NewErrorToolResult("path is required", call)
	}
	if params.Content == nil {
		return llmstream.NewErrorToolResult("content is required", call)
	}
	absPath, relPath, normErr := NormalizePath(params.Path, t.sandboxAbsDir, WantPathTypeFile, false)
	if normErr != nil {
		return NewToolErrorResult(call, normErr.Error(), normErr)
	}
	if t.authorizer != nil {
		if authErr := t.authorizer.IsAuthorizedForWrite(params.RequestPermission, "", ToolNameWrite, absPath); authErr != nil {
			return NewToolErrorResult(call, authErr.Error(), authErr)
		}
	}
	parentDir := filepath.Dir(absPath)
	if mkErr := os.MkdirAll(parentDir, 0o755); mkErr != nil {
		return NewToolErrorResult(call, mkErr.Error(), mkErr)
	}
	if writeErr := os.WriteFile(absPath, []byte(*params.Content), 0o644); writeErr != nil {
		return NewToolErrorResult(call, writeErr.Error(), writeErr)
	}
	displayPath := relPath
	if displayPath == "" {
		displayPath = absPath
	}
	return llmstream.ToolResult{
		CallID: call.CallID,
		Name:   call.Name,
		Type:   call.Type,
		Result: fmt.Sprintf("Wrote file: %s", displayPath),
	}
}
