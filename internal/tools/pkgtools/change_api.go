package pkgtools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/codeunit"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/lints"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/codalotl/codalotl/internal/tools/coretools"
	"github.com/codalotl/codalotl/internal/tools/toolsetinterface"
)

//go:embed change_api.md
var descriptionChangeAPI string

// ToolNameChangeAPI is the registered name of the change_api tool.
const ToolNameChangeAPI = "change_api"

// This mirrors internal/agentbuilder.AgentPackageModeDefaultContext without importing that package and creating an import cycle.
const changeAPIAgentName = "package_mode_default_context"

var changeAPIPresenterInstance llmstream.Presenter = changeAPIPresenter{}
var subAgentCreatorFromContext = agent.SubAgentCreatorFromContext

// The toolChangeAPI type implements the change_api tool for direct upstream packages.
type toolChangeAPI struct {
	sandboxAbsDir string                        // The sandbox root is used to resolve package paths and constrain changes.
	authorizer    authdomain.Authorizer         // The authorizer controls current-package reads and upstream-package writes.
	toolset       toolsetinterface.Toolset      // The toolset is retained for compatibility with existing builders.
	agentInvoker  toolsetinterface.AgentInvoker // The agent invoker creates or invokes the package-update subagent.
	model         llmmodel.ModelID              // The model is used by the package-update subagent.

	// pkgDirAbsPath is the package directory of the agent that is invoking this tool. The tool only allows changing packages that this package directly imports.
	pkgDirAbsPath string

	// The lint steps are run by the package-update subagent after changes.
	lintSteps []lints.Step
}

// The changeAPIParams type contains JSON parameters for the change_api tool.
type changeAPIParams struct {
	Path         string `json:"path"`         // Path identifies the direct upstream package to change.
	Instructions string `json:"instructions"` // Instructions describe the requested API change and the reason for it.
}

// ChangeAPIToolOptions configures optional dependencies for NewChangeAPITool.
type ChangeAPIToolOptions struct {
	AgentInvoker toolsetinterface.AgentInvoker // AgentInvoker invokes subagents; nil makes change_api unavailable.
}

// The changeAPIPresenter type formats change_api tool progress and results.
type changeAPIPresenter struct{}

// NewChangeAPITool creates a tool that can update upstream packages that the current package directly imports.
//
// authorizer should be a sandbox authorizer (not a package-jail authorizer). If the calling agent is jailed, pass authorizer.WithoutCodeUnit().
//
// toolset is retained for compatibility with existing builders; registry-backed subagent invocation is driven by AgentInvoker.
func NewChangeAPITool(pkgDirAbsPath string, authorizer authdomain.Authorizer, toolset toolsetinterface.Toolset, model llmmodel.ModelID, lintSteps []lints.Step, options ...ChangeAPIToolOptions) llmstream.Tool {
	sandboxAbsDir := authorizer.SandboxDir()
	var option ChangeAPIToolOptions
	if len(options) > 0 {
		option = options[0]
	}
	return &toolChangeAPI{
		sandboxAbsDir: sandboxAbsDir,
		authorizer:    authorizer,
		toolset:       toolset,
		agentInvoker:  option.AgentInvoker,
		model:         model,
		pkgDirAbsPath: filepath.Clean(pkgDirAbsPath),
		lintSteps:     lintSteps,
	}
}

// Name returns the registered tool name, "change_api".
func (t *toolChangeAPI) Name() string {
	return ToolNameChangeAPI
}

// Presenter returns the presenter that formats change_api progress and results.
func (t *toolChangeAPI) Presenter() llmstream.Presenter {
	return changeAPIPresenterInstance
}

// SubagentFinalMessage hides final messages from change_api descendant subagents.
func (p changeAPIPresenter) SubagentFinalMessage(llmstream.ToolCall, string, string) llmstream.Block {
	return nil
}

// Present returns the change_api presentation for call and result. It appends progress, renders a changing or changed summary for the target package, and shows
// instructions while in progress or successful result output after completion.
func (p changeAPIPresenter) Present(call llmstream.ToolCall, result *llmstream.ToolResult) llmstream.Presentation {
	action := "Changing API"
	if result != nil {
		action = "Changed API"
	}

	path, instructions, ok := changeAPIPresenterParamsFromCall(call)
	presentation := llmstream.Presentation{
		Behavior: llmstream.CompletionBehaviorAppend,
		Summary:  changeAPIPresenterSummary(action, call, path, ok),
	}
	if result == nil {
		if strings.TrimSpace(instructions) != "" && ok {
			presentation.Body = llmstream.Paragraph{
				Lines: []llmstream.Line{{
					Segments: []llmstream.Segment{
						{Text: instructions, Role: llmstream.RoleAccent},
					},
				}},
			}
		}
		return presentation
	}

	if body, ok := pkgToolResultOutput(*result); ok {
		presentation.Body = body
	}
	return presentation
}

func changeAPIPresenterSummary(action string, call llmstream.ToolCall, path string, ok bool) llmstream.Line {
	if ok {
		return llmstream.Line{
			JoinWithSpace: true,
			Segments: []llmstream.Segment{
				{Text: action, Role: llmstream.RoleAction},
				{Text: "in", Role: llmstream.RoleAccent},
				{Text: path, Role: llmstream.RoleNormal},
			},
		}
	}

	return pkgToolPresenterFallbackSummary(call)
}

func changeAPIPresenterParamsFromCall(call llmstream.ToolCall) (path string, instructions string, ok bool) {
	var params changeAPIParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return "", "", false
	}

	path = strings.TrimSpace(params.Path)
	instructions = strings.TrimSpace(params.Instructions)
	if path == "" {
		return "", "", false
	}
	return path, instructions, true
}

// Info returns the LLM-facing metadata for the change_api tool, including the required upstream package path and change instructions.
func (t *toolChangeAPI) Info() llmstream.ToolInfo {
	return llmstream.ToolInfo{
		Name:        ToolNameChangeAPI,
		Description: strings.TrimSpace(descriptionChangeAPI),
		Parameters: map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "A Go package directory (relative to the sandbox) or a Go import path. Must resolve to an upstream package that the current package directly imports.",
			},
			"instructions": map[string]any{
				"type":        "string",
				"description": "What to change and why. Include enough context for a new agent to update the package safely.",
			},
		},
		Required: []string{"path", "instructions"},
	}
}

// Run executes a change_api tool call for a direct upstream package.
//
// The call input must be a JSON object with "path" and "instructions" strings. Path must name a package by sandbox-relative directory or Go import path, must resolve
// inside the sandbox, must be imported directly by the invoking package, and must not name the invoking package itself. Run authorizes the read and write operations
// when an authorizer is configured, then invokes the package update subagent scoped to the target package.
//
// The result contains the subagent's final answer, trimmed of surrounding whitespace. Run returns an error tool result for invalid input, rejected package paths,
// authorization failures, package-loading failures, or subagent failures.
func (t *toolChangeAPI) Run(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
	var params changeAPIParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return coretools.NewToolErrorResult(call, fmt.Sprintf("error parsing parameters: %s", err), err)
	}

	pathParam := strings.TrimSpace(params.Path)
	if pathParam == "" {
		return llmstream.NewErrorToolResult("path is required", call)
	}
	instructions := strings.TrimSpace(params.Instructions)
	if instructions == "" {
		return llmstream.NewErrorToolResult("instructions is required", call)
	}

	mod, err := gocode.NewModule(t.sandboxAbsDir)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	currentPkgAbsDir := t.pkgDirAbsPath
	if !filepath.IsAbs(currentPkgAbsDir) {
		currentPkgAbsDir = filepath.Join(t.sandboxAbsDir, currentPkgAbsDir)
	}
	currentPkgAbsDir = filepath.Clean(currentPkgAbsDir)

	if t.authorizer != nil {
		if authErr := t.authorizer.IsAuthorizedForRead(false, "", ToolNameChangeAPI, currentPkgAbsDir); authErr != nil {
			return coretools.NewToolErrorResult(call, authErr.Error(), authErr)
		}
	}

	relCurrentDir, err := filepath.Rel(mod.AbsolutePath, currentPkgAbsDir)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}
	if relCurrentDir == ".." || strings.HasPrefix(relCurrentDir, ".."+string(filepath.Separator)) {
		return coretools.NewToolErrorResult(call, fmt.Sprintf("current package directory %q is outside module %q", currentPkgAbsDir, mod.AbsolutePath), nil)
	}
	if relCurrentDir == "." {
		relCurrentDir = ""
	}

	currentPkg, err := mod.LoadPackageByRelativeDir(filepath.ToSlash(relCurrentDir))
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	resolved, err := resolveToolPackageRef(mod, pathParam)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	if err := validateResolvedPackageRefInSandbox(t.sandboxAbsDir, pathParam, resolved); err != nil {
		return llmstream.NewErrorToolResult(
			fmt.Sprintf("%s; change_api can only modify packages within the sandbox", err.Error()),
			call,
		)
	}
	if resolved.ImportPath == currentPkg.ImportPath {
		return llmstream.NewErrorToolResult("path must refer to an upstream package (not the current package)", call)
	}

	if currentPkg.ImportPaths == nil {
		return coretools.NewToolErrorResult(call, "unable to determine direct imports for current package", nil)
	}
	if _, ok := currentPkg.ImportPaths[resolved.ImportPath]; !ok {
		return llmstream.NewErrorToolResult(
			fmt.Sprintf("path %q resolves to %q, which is not a direct import of the current package %q", pathParam, resolved.ImportPath, currentPkg.ImportPath),
			call,
		)
	}

	targetAbsDir := filepath.Clean(resolved.PackageAbsDir)

	if t.authorizer != nil {
		if authErr := t.authorizer.IsAuthorizedForWrite(false, "", ToolNameChangeAPI, targetAbsDir); authErr != nil {
			return coretools.NewToolErrorResult(call, authErr.Error(), authErr)
		}
	}

	agentCreator, err := subAgentCreatorFromContextSafe(ctx)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	// Ensure the target package exists and is loadable (helps produce better errors than failing later).
	if _, err := loadPackageForResolved(mod, resolved.ModuleAbsDir, resolved.PackageAbsDir, resolved.PackageRelDir, resolved.ImportPath); err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	unit, err := codeunit.NewCodeUnit(fmt.Sprintf("package %s", resolved.ImportPath), targetAbsDir)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}
	unit.IncludeEntireSubtree()

	pkgAuthorizer := authdomain.NewCodeUnitAuthorizer(unit, t.authorizer)

	answer, err := invokeChangeAPIAgent(
		ctx,
		t.agentInvoker,
		agentCreator,
		t.sandboxAbsDir,
		pkgAuthorizer,
		targetAbsDir,
		t.model,
		t.lintSteps,
		t.agentInvoker,
		instructions,
	)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	return llmstream.ToolResult{
		CallID: call.CallID,
		Name:   call.Name,
		Type:   call.Type,
		Result: strings.TrimSpace(answer),
	}
}

func subAgentCreatorFromContextSafe(ctx context.Context) (creator agent.SubAgentCreator, err error) {
	defer func() {
		if r := recover(); r != nil {
			creator = nil
			err = fmt.Errorf("unable to create subagent")
		}
	}()

	creator = subAgentCreatorFromContext(ctx)
	if creator == nil {
		return nil, fmt.Errorf("unable to create subagent")
	}
	return creator, nil
}

// invokeChangeAPIAgent runs the change_api subagent for packageAbsDir and returns its final assistant text. It propagates the caller sandbox, package authorizer,
// model, lint steps, and nested-agent invoker to the subagent.
func invokeChangeAPIAgent(ctx context.Context, invoker toolsetinterface.AgentInvoker, agentCreator agent.AgentCreator, sandboxAbsDir string, pkgAuthorizer authdomain.Authorizer, packageAbsDir string, model llmmodel.ModelID, lintSteps []lints.Step, nestedAgentInvoker toolsetinterface.AgentInvoker, instructions string) (string, error) {
	if invoker == nil {
		return "", fmt.Errorf("change_api agent unavailable")
	}

	req := toolsetinterface.InvokeRequest{
		AgentCreator:     agentCreator,
		CallerAuthorizer: pkgAuthorizer,
		CallerSandboxDir: sandboxAbsDir,
		ToolOptions: toolsetinterface.Options{
			SandboxDir:   sandboxAbsDir,
			GoPkgAbsDir:  packageAbsDir,
			Model:        model,
			LintSteps:    lintSteps,
			AgentInvoker: nestedAgentInvoker,
		},
		Messages: []string{instructions},
	}

	events, err := invoker.Invoke(ctx, changeAPIAgentName, req)
	if err != nil {
		return "", err
	}

	return agent.CollectFinalAssistantText(ctx, events)
}
