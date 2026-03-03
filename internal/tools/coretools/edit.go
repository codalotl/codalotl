package coretools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"github.com/codalotl/codalotl/internal/applypatch"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"strings"
)

//go:embed edit.md
var descriptionEdit string

const ToolNameEdit = "edit"

type toolEdit struct {
	sandboxAbsDir string
	authorizer    authdomain.Authorizer
	postChecks    *EditPostChecks
}

type ParamsEdit struct {
	Path              string  `json:"path"`
	OldText           *string `json:"old_text"`
	NewText           *string `json:"new_text"`
	ReplaceAll        bool    `json:"replace_all"`
	RequestPermission bool    `json:"request_permission"`
}

func NewEditTool(authorizer authdomain.Authorizer, postChecks ...*EditPostChecks) llmstream.Tool {
	sandboxAbsDir := authorizer.SandboxDir()
	var configuredPostChecks *EditPostChecks
	if len(postChecks) > 0 {
		configuredPostChecks = postChecks[0]
	}
	return &toolEdit{
		sandboxAbsDir: sandboxAbsDir,
		authorizer:    authorizer,
		postChecks:    configuredPostChecks,
	}
}

func (t *toolEdit) Name() string {
	return ToolNameEdit
}

func (t *toolEdit) Info() llmstream.ToolInfo {
	return llmstream.ToolInfo{
		Name:        ToolNameEdit,
		Description: strings.TrimSpace(descriptionEdit),
		Parameters: map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "The path of the file to edit (absolute, or relative to sandbox dir)",
			},
			"old_text": map[string]any{
				"type":        "string",
				"description": "Text to find in the file",
			},
			"new_text": map[string]any{
				"type":        "string",
				"description": "Text to replace old_text with",
			},
			"replace_all": map[string]any{
				"type":        "boolean",
				"description": "If true, replace all matches of old_text; otherwise replace one unambiguous match",
			},
			"request_permission": map[string]any{
				"type":        "boolean",
				"description": "Optionally request the user's permission to run this command. Set to true for material access outside sandbox dir",
			},
		},
		Required: []string{"path", "old_text", "new_text"},
	}
}

func (t *toolEdit) Run(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
	var params ParamsEdit
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return NewToolErrorResult(call, fmt.Sprintf("error parsing parameters: %s", err), err)
	}
	if params.Path == "" {
		return llmstream.NewErrorToolResult("path is required", call)
	}
	if params.OldText == nil {
		return llmstream.NewErrorToolResult("old_text is required", call)
	}
	if *params.OldText == "" {
		return llmstream.NewErrorToolResult("old_text must not be empty", call)
	}
	if params.NewText == nil {
		return llmstream.NewErrorToolResult("new_text is required", call)
	}

	absPath, relPath, normErr := NormalizePath(params.Path, t.sandboxAbsDir, WantPathTypeFile, true)
	if normErr != nil {
		return NewToolErrorResult(call, normErr.Error(), normErr)
	}
	if t.authorizer != nil {
		if authErr := t.authorizer.IsAuthorizedForWrite(params.RequestPermission, "", ToolNameEdit, absPath); authErr != nil {
			return NewToolErrorResult(call, authErr.Error(), authErr)
		}
	}
	if _, replaceErr := applypatch.Replace(absPath, *params.OldText, *params.NewText, params.ReplaceAll); replaceErr != nil {
		return NewToolErrorResult(call, replaceErr.Error(), replaceErr)
	}

	displayPath := relPath
	if displayPath == "" {
		displayPath = absPath
	}
	result := fmt.Sprintf("Edited file: %s", displayPath)
	if shouldRunPostChecks(t.postChecks) {
		extraOutputs, err := runPostChecks(ctx, t.sandboxAbsDir, t.postChecks, []string{absPath})
		if err != nil {
			result = result + "\n\nPost edit checks errored: " + err.Error()
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
