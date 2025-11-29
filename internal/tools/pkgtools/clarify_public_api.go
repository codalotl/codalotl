package pkgtools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/llmstream"
	clarify "github.com/codalotl/codalotl/internal/subagents/clarifydocs"
	"github.com/codalotl/codalotl/internal/tools/auth"
	"github.com/codalotl/codalotl/internal/tools/coretools"
	"github.com/codalotl/codalotl/internal/tools/toolsetinterface"
	"path/filepath"
	"strings"
)

//go:embed clarify_public_api.md
var descriptionClarifyPublicAPI string

const ToolNameClarifyPublicAPI = "clarify_public_api"

type toolClarifyPublicAPI struct {
	sandboxAbsDir string
	authorizer    auth.Authorizer
	toolset       toolsetinterface.Toolset
}

type clarifyPublicAPIParams struct {
	Path       string `json:"path"`
	Identifier string `json:"identifier"`
	Question   string `json:"question"`
}

// authorizer is what the **subagent** is authorized to do, which is usually more than a package-jailed agent.
func NewClarifyPublicAPITool(sandboxAbsDir string, authorizer auth.Authorizer, toolset toolsetinterface.Toolset) llmstream.Tool {
	return &toolClarifyPublicAPI{
		sandboxAbsDir: filepath.Clean(sandboxAbsDir),
		authorizer:    authorizer,
		toolset:       toolset,
	}
}

func (t *toolClarifyPublicAPI) Name() string {
	return ToolNameClarifyPublicAPI
}

func (t *toolClarifyPublicAPI) Info() llmstream.ToolInfo {
	return llmstream.ToolInfo{
		Name:        ToolNameClarifyPublicAPI,
		Description: strings.TrimSpace(descriptionClarifyPublicAPI),
		Parameters: map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Filesystem path (absolute or relative to sandbox) containing the public API docs.",
			},
			"identifier": map[string]any{
				"type":        "string",
				"description": "The identifier needing clarification.",
			},
			"question": map[string]any{
				"type":        "string",
				"description": "The specific clarification question.",
			},
		},
		Required: []string{"path", "identifier", "question"},
	}
}

func (t *toolClarifyPublicAPI) Run(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
	var params clarifyPublicAPIParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return coretools.NewToolErrorResult(call, fmt.Sprintf("error parsing parameters: %s", err), err)
	}

	pathParam := strings.TrimSpace(params.Path)
	if pathParam == "" {
		return llmstream.NewErrorToolResult("path is required", call)
	}
	if params.Identifier == "" {
		return llmstream.NewErrorToolResult("identifier is required", call)
	}
	if params.Question == "" {
		return llmstream.NewErrorToolResult("question is required", call)
	}

	absPath := pathParam
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(t.sandboxAbsDir, absPath)
	}
	absPath = filepath.Clean(absPath)

	if t.authorizer != nil {
		if authErr := t.authorizer.IsAuthorizedForRead(false, "", ToolNameClarifyPublicAPI, t.sandboxAbsDir, absPath); authErr != nil {
			return coretools.NewToolErrorResult(call, authErr.Error(), authErr)
		}
	}

	agentCreator := agent.SubAgentCreatorFromContext(ctx)
	if agentCreator == nil {
		return coretools.NewToolErrorResult(call, "unable to create subagent", nil)
	}

	answer, err := clarify.ClarifyAPI(ctx, agentCreator, t.sandboxAbsDir, t.authorizer, t.toolset, absPath, params.Identifier, params.Question)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	return llmstream.ToolResult{
		CallID: call.CallID,
		Name:   call.Name,
		Type:   call.Type,
		Result: answer,
	}
}
