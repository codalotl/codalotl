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

// ToolNameWrite is the registered name of the write tool.
const ToolNameWrite = "write"

// The toolWrite type implements the write tool by creating or replacing file contents.
type toolWrite struct {
	sandboxAbsDir string                // This is the absolute sandbox root for path resolution and post-checks.
	authorizer    authdomain.Authorizer // This authorizes writes to the target file before writing.
	postChecks    *WritePostChecks      // This configures optional diagnostics and lint hooks after a successful write.
}

// ParamsWrite contains the JSON arguments for the write tool.
type ParamsWrite struct {
	// Path is the file to create or replace. Relative paths are resolved from the sandbox root, and parent directories are created as needed.
	Path string `json:"path"`

	// Content is the complete file content to write. A nil Content is rejected as missing, but an empty string is valid.
	Content *string `json:"content"`

	// RequestPermission asks for approval to write the file when policy requires it.
	RequestPermission bool `json:"request_permission"`
}

// NewWriteTool returns a write tool that creates or replaces files using authorizer for sandbox resolution and write authorization. The authorizer must be non-nil.
// If postChecks is provided, only the first value is used to configure post-write checks.
func NewWriteTool(authorizer authdomain.Authorizer, postChecks ...*WritePostChecks) llmstream.Tool {
	sandboxAbsDir := authorizer.SandboxDir()
	var configuredPostChecks *WritePostChecks
	if len(postChecks) > 0 {
		configuredPostChecks = postChecks[0]
	}
	return &toolWrite{
		sandboxAbsDir: sandboxAbsDir,
		authorizer:    authorizer,
		postChecks:    configuredPostChecks,
	}
}

// Name returns the registered name of the write tool.
func (t *toolWrite) Name() string {
	return ToolNameWrite
}

// Presenter returns the presenter used to display write calls as semantic file diffs.
func (t *toolWrite) Presenter() llmstream.Presenter {
	return writePresenterInstance
}

// Info returns the tool metadata for write, including its embedded description and JSON parameters. The returned schema requires path and content, and accepts request_permission
// for authorization escalation.
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

// Run executes a write tool call by parsing its JSON parameters, validating and authorizing the target file, creating parent directories, and writing the complete
// content. On success it returns the written path and appends configured post-check output; input, authorization, and filesystem failures are returned as tool errors.
func (t *toolWrite) Run(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
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
	result := fmt.Sprintf("Wrote file: %s", displayPath)
	if shouldRunPostChecks(t.postChecks) {
		extraOutputs, err := runPostChecks(ctx, t.sandboxAbsDir, t.postChecks, []string{absPath})
		if err != nil {
			result = result + "\n\nPost write checks errored: " + err.Error()
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
