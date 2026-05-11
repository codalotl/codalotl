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
	"github.com/codalotl/codalotl/internal/gocas"
	"github.com/codalotl/codalotl/internal/gocas/casclarify"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/lints"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/q/cas"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/codalotl/codalotl/internal/tools/coretools"
	"github.com/codalotl/codalotl/internal/tools/toolsetinterface"
)

//go:embed clarify_public_api.md
var descriptionClarifyPublicAPI string

const ToolNameClarifyPublicAPI = "clarify_public_api"

type toolClarifyPublicAPI struct {
	sandboxAbsDir       string
	authorizer          authdomain.Authorizer
	agentInvoker        toolsetinterface.AgentInvoker
	model               llmmodel.ModelID
	originPackageAbsDir string
}

type clarifyPublicAPIParams struct {
	Path       string `json:"path"`
	Identifier string `json:"identifier"`
	Question   string `json:"question"`
}

// ClarifyPublicAPIToolOptions configures NewClarifyPublicAPITool.
type ClarifyPublicAPIToolOptions struct {
	AgentInvoker toolsetinterface.AgentInvoker // AgentInvoker invokes the clarification subagent.
	Model        llmmodel.ModelID              // Model selects the model used by the clarification subagent.
	LintSteps    []lints.Step                  // LintSteps are accepted for consistency with other package tools; clarify_public_api does not run lint steps.

	// OriginPackageAbsDir identifies the package that initiated the clarification for CAS metadata. It does not constrain target-package reads; the clarification subagent
	// is jailed to the resolved target package. If empty, NewClarifyPublicAPITool uses authorizer.CodeUnitDir() when present.
	OriginPackageAbsDir string
}

var clarifyPublicAPIPresenterInstance llmstream.Presenter = clarifyPublicAPIPresenter{}

type clarifyPublicAPIPresenter struct{}

// NewClarifyPublicAPITool returns a tool that asks a read-only subagent to clarify a package's public API. The authorizer supplies the caller sandbox, caller authorization
// context, sandbox-package read authorization, and CAS write authorization. It may be a base authorizer or a code-unit authorizer; the subagent is run with the
// caller code-unit removed and then jailed to the resolved target package.
func NewClarifyPublicAPITool(authorizer authdomain.Authorizer, toolset toolsetinterface.Toolset, options ...ClarifyPublicAPIToolOptions) llmstream.Tool {
	sandboxAbsDir := authorizer.SandboxDir()
	var option ClarifyPublicAPIToolOptions
	if len(options) > 0 {
		option = options[0]
	}
	originPackageAbsDir := option.OriginPackageAbsDir
	if originPackageAbsDir == "" && authorizer.CodeUnitDir() != "" {
		originPackageAbsDir = authorizer.CodeUnitDir()
	}
	return &toolClarifyPublicAPI{
		sandboxAbsDir:       sandboxAbsDir,
		authorizer:          authorizer,
		agentInvoker:        option.AgentInvoker,
		model:               option.Model,
		originPackageAbsDir: originPackageAbsDir,
	}
}

func (t *toolClarifyPublicAPI) Name() string {
	return ToolNameClarifyPublicAPI
}

func (t *toolClarifyPublicAPI) Presenter() llmstream.Presenter {
	return clarifyPublicAPIPresenterInstance
}

func (p clarifyPublicAPIPresenter) SubagentFinalMessage(llmstream.ToolCall, string, string) llmstream.Block {
	return nil
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

	content, payloadErr, isPayload := pkgToolResultPayloadContent(result)
	if isPayload && payloadErr != "" {
		return "", false
	}
	if content == "" {
		return "", false
	}
	return content, true
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

	moduleAbsDir, packageAbsDir, packageRelDir, importPath, err := resolvePackagePath(mod, params.Path)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}
	resolved := resolvedPackageRef{
		ModuleAbsDir:  moduleAbsDir,
		PackageAbsDir: packageAbsDir,
		PackageRelDir: packageRelDir,
		ImportPath:    importPath,
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

	if err := t.recordClarifyCAS(mod, resolved, params.Identifier, params.Question, answer); err != nil {
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

func (t *toolClarifyPublicAPI) recordClarifyCAS(mod *gocode.Module, resolved resolvedPackageRef, identifier string, question string, answer string) error {
	if mod == nil {
		return fmt.Errorf("module required")
	}
	if !resolved.isWithinSandbox(t.sandboxAbsDir) {
		return nil
	}

	targetPkg, err := loadPackageForResolved(mod, resolved.ModuleAbsDir, resolved.PackageAbsDir, resolved.PackageRelDir, resolved.ImportPath)
	if err != nil {
		return err
	}

	originPackage, err := clarifyOriginPackageIdentity(mod, t.originPackageAbsDir)
	if err != nil {
		return err
	}

	casRootAbsDir := clarifyCASRoot(mod.AbsolutePath)
	if err := authorizeClarifyCASWrite(t.authorizer, casRootAbsDir); err != nil {
		return err
	}

	entry := casclarify.Entry{
		OriginPackage: originPackage,
		TargetPackage: targetPkg.ImportPath,
		Identifier:    identifier,
		Question:      question,
		Answer:        answer,
	}
	if err := casclarify.Append(newClarifyCASDB(mod), targetPkg, entry); err != nil {
		return fmt.Errorf("record clarify CAS: %w", err)
	}
	return nil
}

func clarifyOriginPackageIdentity(mod *gocode.Module, originPackageAbsDir string) (string, error) {
	if originPackageAbsDir == "" {
		return "", nil
	}
	if !filepath.IsAbs(originPackageAbsDir) {
		originPackageAbsDir = filepath.Join(mod.AbsolutePath, originPackageAbsDir)
	}
	if !isWithinDir(mod.AbsolutePath, originPackageAbsDir) {
		return "", fmt.Errorf("origin package directory %q is outside module %q", originPackageAbsDir, mod.AbsolutePath)
	}

	originMod, err := gocode.NewModule(originPackageAbsDir)
	if err != nil {
		return "", fmt.Errorf("resolve origin module: %w", err)
	}
	if !isWithinDir(mod.AbsolutePath, originMod.AbsolutePath) {
		return "", fmt.Errorf("origin module %q is outside module %q", originMod.AbsolutePath, mod.AbsolutePath)
	}

	relDir, err := filepath.Rel(originMod.AbsolutePath, originPackageAbsDir)
	if err != nil {
		return "", err
	}
	pkg, err := originMod.LoadPackageByRelativeDir(filepath.ToSlash(relDir))
	if err != nil {
		return "", fmt.Errorf("load origin package: %w", err)
	}
	return pkg.ImportPath, nil
}

func authorizeClarifyCASWrite(authorizer authdomain.Authorizer, casRootAbsDir string) error {
	if authorizer == nil {
		return nil
	}
	return authorizer.WithoutCodeUnit().IsAuthorizedForWrite(true, "record clarify_public_api answer in .codalotl/cas", ToolNameClarifyPublicAPI, casRootAbsDir)
}

func clarifyCASRoot(moduleAbsDir string) string {
	return filepath.Join(moduleAbsDir, ".codalotl", "cas")
}

func newClarifyCASDB(mod *gocode.Module) *gocas.DB {
	return &gocas.DB{
		BaseDir: mod.AbsolutePath,
		DB: cas.DB{
			AbsRoot: clarifyCASRoot(mod.AbsolutePath),
		},
	}
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

	unit, err := codeunit.DefaultGoCodeUnit(packageAbsDir)
	if err != nil {
		return nil, fmt.Errorf("build code unit: %w", err)
	}
	return authdomain.NewCodeUnitAuthorizer(unit, baseAuthorizer), nil
}
