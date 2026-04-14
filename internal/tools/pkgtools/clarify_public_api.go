package pkgtools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/codeunit"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/codalotl/codalotl/internal/tools/coretools"
	"github.com/codalotl/codalotl/internal/tools/toolsetinterface"
)

//go:embed clarify_public_api.md
var descriptionClarifyPublicAPI string

const ToolNameClarifyPublicAPI = "clarify_public_api"

type toolClarifyPublicAPI struct {
	sandboxAbsDir string
	authorizer    authdomain.Authorizer
	agentInvoker  toolsetinterface.AgentInvoker
	model         llmmodel.ModelID
}

type clarifyPublicAPIParams struct {
	Path       string `json:"path"`
	Identifier string `json:"identifier"`
	Question   string `json:"question"`
}

type ClarifyPublicAPIToolOptions struct {
	AgentInvoker toolsetinterface.AgentInvoker
	Model        llmmodel.ModelID
}

var clarifyPublicAPIPresenterInstance llmstream.Presenter = clarifyPublicAPIPresenter{}

type clarifyPublicAPIPresenter struct{}

// authorizer is the fallback authorizer the clarify subagent should use underneath its target-package jail.
func NewClarifyPublicAPITool(authorizer authdomain.Authorizer, toolset toolsetinterface.Toolset, options ...ClarifyPublicAPIToolOptions) llmstream.Tool {
	sandboxAbsDir := authorizer.SandboxDir()
	var option ClarifyPublicAPIToolOptions
	if len(options) > 0 {
		option = options[0]
	}
	return &toolClarifyPublicAPI{
		sandboxAbsDir: sandboxAbsDir,
		authorizer:    authorizer,
		agentInvoker:  option.AgentInvoker,
		model:         option.Model,
	}
}

func (t *toolClarifyPublicAPI) Name() string {
	return ToolNameClarifyPublicAPI
}

func (t *toolClarifyPublicAPI) Presenter() llmstream.Presenter {
	return clarifyPublicAPIPresenterInstance
}

func (p clarifyPublicAPIPresenter) SubagentEventPolicy(call llmstream.ToolCall) llmstream.SubagentEventPolicy {
	return llmstream.SubagentEventPolicyHideFinalMessage
}

func (p clarifyPublicAPIPresenter) Present(call llmstream.ToolCall, result *llmstream.ToolResult) llmstream.Presentation {
	action := "Clarifying API"
	if result != nil {
		action = "Clarified API"
	}

	identifier, path, question, ok := clarifyPublicAPIPresenterParamsFromCall(call)
	presentation := llmstream.Presentation{
		Behavior: llmstream.CompletionBehaviorAppend,
		Summary:  clarifyPublicAPIPresenterSummary(action, call, identifier, path, ok),
	}

	if result == nil {
		if body, ok := pkgToolPresenterOutput(question); ok {
			presentation.Body = body
		}
		return presentation
	}

	if content, ok := clarifyPublicAPIPresenterResultContent(*result); ok {
		if body, ok := pkgToolPresenterOutput(content); ok {
			presentation.Body = body
		}
	}
	return presentation
}

func clarifyPublicAPIPresenterSummary(action string, call llmstream.ToolCall, identifier string, path string, ok bool) llmstream.Line {
	if !ok {
		return pkgToolPresenterFallbackSummary(call)
	}

	segments := []llmstream.Segment{
		{Text: action, Role: llmstream.RoleAction},
	}
	if identifier != "" {
		segments = append(segments, llmstream.Segment{Text: identifier, Role: llmstream.RoleNormal})
	}
	if path != "" {
		segments = append(segments,
			llmstream.Segment{Text: "in", Role: llmstream.RoleAccent},
			llmstream.Segment{Text: path, Role: llmstream.RoleNormal},
		)
	}
	return llmstream.Line{
		JoinWithSpace: true,
		Segments:      segments,
	}
}

func clarifyPublicAPIPresenterParamsFromCall(call llmstream.ToolCall) (identifier string, path string, question string, ok bool) {
	var params clarifyPublicAPIParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return "", "", "", false
	}

	identifier = strings.TrimSpace(params.Identifier)
	path = strings.TrimSpace(params.Path)
	question = strings.TrimSpace(params.Question)
	if identifier == "" && path == "" && question == "" {
		return "", "", "", false
	}
	return identifier, path, question, true
}

func clarifyPublicAPIPresenterResultContent(result llmstream.ToolResult) (string, bool) {
	if result.IsError {
		return "", false
	}

	trimmed := strings.TrimSpace(result.Result)
	if trimmed == "" {
		return "", false
	}

	var payload struct {
		Content string `json:"content"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal([]byte(trimmed), &payload); err == nil {
		if strings.TrimSpace(payload.Error) != "" {
			return "", false
		}
		content := strings.TrimSpace(payload.Content)
		if content == "" {
			return "", false
		}
		return content, true
	}

	return trimmed, true
}

func (t *toolClarifyPublicAPI) Info() llmstream.ToolInfo {
	return llmstream.ToolInfo{
		Name:        ToolNameClarifyPublicAPI,
		Description: strings.TrimSpace(descriptionClarifyPublicAPI),
		Parameters: map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "A Go package directory (relative to the sandbox) or a Go import path.",
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

	if params.Path == "" {
		return llmstream.NewErrorToolResult("path is required", call)
	}
	if params.Identifier == "" {
		return llmstream.NewErrorToolResult("identifier is required", call)
	}
	if params.Question == "" {
		return llmstream.NewErrorToolResult("question is required", call)
	}

	mod, err := gocode.NewModule(t.sandboxAbsDir)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	moduleAbsDir, packageAbsDir, _, importPath, err := resolvePackagePath(mod, params.Path)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	effectiveSandboxAbsDir := t.sandboxAbsDir
	baseAuthorizer := t.authorizer
	if baseAuthorizer != nil {
		baseAuthorizer = baseAuthorizer.WithoutCodeUnit()
	}
	if !isWithinDir(t.sandboxAbsDir, packageAbsDir) {
		// If the resolved package is outside of the primary sandbox, treat its module (or stdlib root)
		// as the sandbox root for relative path resolution in the clarify subagent.
		if moduleAbsDir != "" {
			effectiveSandboxAbsDir = moduleAbsDir
		} else {
			stdRootAbsDir, _ := stdlibRootAndRel(packageAbsDir, importPath)
			if stdRootAbsDir != "" {
				effectiveSandboxAbsDir = stdRootAbsDir
			}
		}

		// Some authorizers enforce sandbox membership. If we can, clone it with an updated sandbox
		// to allow read access to the resolved dependency/stdlib package while still confining reads.
		if baseAuthorizer != nil && effectiveSandboxAbsDir != "" {
			if updated, updErr := authdomain.WithUpdatedSandbox(baseAuthorizer, effectiveSandboxAbsDir); updErr == nil {
				baseAuthorizer = updated
			}
		}
	}

	effectiveAuthorizer, err := newClarifyTargetAuthorizer(baseAuthorizer, packageAbsDir)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	agentPath, err := packagePathForSandbox(effectiveSandboxAbsDir, packageAbsDir)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	if t.authorizer != nil && isWithinDir(t.sandboxAbsDir, packageAbsDir) {
		// Only prompt/deny for sandbox reads; resolved dependency/stdlib packages are always readable.
		if authErr := t.authorizer.IsAuthorizedForRead(false, "", ToolNameClarifyPublicAPI, packageAbsDir); authErr != nil {
			return coretools.NewToolErrorResult(call, authErr.Error(), authErr)
		}
	}

	agentCreator, err := subAgentCreatorFromContextSafe(ctx)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	answer, err := invokeClarifyAgent(
		ctx,
		t.agentInvoker,
		agentCreator,
		t.sandboxAbsDir,
		t.authorizer,
		effectiveSandboxAbsDir,
		effectiveAuthorizer,
		t.model,
		agentPath,
		packageAbsDir,
		params.Identifier,
		params.Question,
	)
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

func invokeClarifyAgent(ctx context.Context, invoker toolsetinterface.AgentInvoker, agentCreator agent.AgentCreator, callerSandboxAbsDir string, callerAuthorizer authdomain.Authorizer, targetSandboxAbsDir string, targetAuthorizer authdomain.Authorizer, model llmmodel.ModelID, path string, packageAbsDir string, identifier string, question string) (string, error) {
	if invoker == nil {
		return "", fmt.Errorf("clarify agent unavailable")
	}

	payload, err := json.Marshal(clarifyPublicAPIParams{
		Path:       path,
		Identifier: identifier,
		Question:   question,
	})
	if err != nil {
		return "", err
	}

	req := toolsetinterface.InvokeRequest{
		AgentCreator:       agentCreator,
		CallerAuthorizer:   callerAuthorizer,
		CallerSandboxDir:   callerSandboxAbsDir,
		OverrideAuthorizer: targetAuthorizer,
		OverrideSandboxDir: targetSandboxAbsDir,
		ToolOptions: toolsetinterface.Options{
			GoPkgAbsDir: packageAbsDir,
			Model:       model,
		},
		Messages: []string{question},
		Payload:  payload,
	}

	events, err := invoker.Invoke(ctx, ToolNameClarifyPublicAPI, req)
	if err != nil {
		return "", err
	}

	return agent.CollectFinalAssistantText(ctx, events)
}

func packagePathForSandbox(sandboxAbsDir string, packageAbsDir string) (string, error) {
	if sandboxAbsDir == "" {
		return "", fmt.Errorf("sandbox directory required")
	}
	if packageAbsDir == "" {
		return "", fmt.Errorf("package path is required")
	}

	rel, err := filepath.Rel(sandboxAbsDir, packageAbsDir)
	if err != nil {
		return "", err
	}
	rel = filepath.ToSlash(rel)
	if rel == "." {
		return ".", nil
	}
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("package %q is outside sandbox %q", packageAbsDir, sandboxAbsDir)
	}
	return rel, nil
}

func newClarifyTargetAuthorizer(baseAuthorizer authdomain.Authorizer, packageAbsDir string) (authdomain.Authorizer, error) {
	if baseAuthorizer == nil {
		return nil, nil
	}
	if packageAbsDir == "" {
		return nil, fmt.Errorf("package path is required")
	}

	unit, err := codeunit.NewCodeUnit(fmt.Sprintf("package %s", packageAbsDir), packageAbsDir)
	if err != nil {
		return nil, fmt.Errorf("build code unit: %w", err)
	}
	if err := unit.IncludeSubtreeUnlessContains("*.go"); err != nil {
		return nil, fmt.Errorf("expand code unit subtree: %w", err)
	}
	if err := includeReachableTestdataDirs(unit); err != nil {
		return nil, fmt.Errorf("include reachable testdata dirs: %w", err)
	}
	return authdomain.NewCodeUnitAuthorizer(unit, baseAuthorizer), nil
}

func includeReachableTestdataDirs(unit *codeunit.CodeUnit) error {
	if unit == nil {
		return nil
	}

	for _, absPath := range unit.IncludedFiles() {
		info, err := os.Stat(absPath)
		if err != nil || !info.IsDir() {
			continue
		}

		testdataPath := filepath.Join(absPath, "testdata")
		tdInfo, err := os.Stat(testdataPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("stat %q: %w", testdataPath, err)
		}
		if !tdInfo.IsDir() || unit.Includes(testdataPath) {
			continue
		}
		if err := unit.IncludeDir(testdataPath, true); err != nil {
			return fmt.Errorf("include %q: %w", testdataPath, err)
		}
	}

	return nil
}
