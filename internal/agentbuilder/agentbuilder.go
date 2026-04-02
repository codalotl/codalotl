package agentbuilder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/codalotl/codalotl/internal/agentregistry"
	"github.com/codalotl/codalotl/internal/detectlang"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/initialcontext"
	"github.com/codalotl/codalotl/internal/lints"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/prompt"
	"github.com/codalotl/codalotl/internal/q/cmdrunner"
	"github.com/codalotl/codalotl/internal/skills"
	"github.com/codalotl/codalotl/internal/tools/coretools"
	"github.com/codalotl/codalotl/internal/tools/exttools"
	"github.com/codalotl/codalotl/internal/tools/pkgtools"
	"github.com/codalotl/codalotl/internal/tools/toolsetinterface"
)

const (
	AgentGeneric                   string = "generic"
	AgentPackageModeNoContext      string = "package_mode_no_context"
	AgentPackageModeDefaultContext string = "package_mode_default_context" // AgentPackageModeDefaultContext adds package initial context before user messages.
	AgentLimitedPackageMode        string = "limited_package_mode"
	agentClarifyPublicAPI                 = pkgtools.ToolNameClarifyPublicAPI
	clarifyRGLines                        = "4"
)

type clarifyPublicAPIRequest struct {
	Path       string `json:"path"`
	Identifier string `json:"identifier"`
	Question   string `json:"question"`
}

// BuildRegistry builds the registry.
func BuildRegistry() (*agentregistry.Registry, error) {
	registry := agentregistry.NewRegistry()

	for toolName, tool := range genericTools() {
		if err := registry.RegisterTool(toolName, tool); err != nil {
			return nil, err
		}
	}

	if err := registry.RegisterAgent(agentregistry.Definition{
		Name:        AgentGeneric,
		Description: "General-purpose agent with core file editing and shell tools.",
		ToolNames: []string{
			coretools.ToolNameReadFile,
			coretools.ToolNameLS,
		},
		ToolsBuilder: buildGenericToolNames,
		SystemPromptBuilder: func(options agentregistry.BuildOptions) (string, error) {
			return prompt.GetBasicPrompt(), nil
		},
	}); err != nil {
		return nil, err
	}

	if err := registry.RegisterAgent(agentregistry.Definition{
		Name:        AgentPackageModeNoContext,
		Description: "Go package-focused agent with package-jail editing, testing, and API analysis tools.",
		ToolNames: []string{
			coretools.ToolNameReadFile,
			coretools.ToolNameLS,
		},
		ToolsBuilder: buildPackageModeToolNames,
		SystemPromptBuilder: func(options agentregistry.BuildOptions) (string, error) {
			return prompt.GetGoPackageModeModePrompt(prompt.GoPackageModePromptKindFull), nil
		},
		AuthPolicy: agentregistry.AuthPolicyPackage,
	}); err != nil {
		return nil, err
	}

	if err := registry.RegisterAgent(agentregistry.Definition{
		Name:        AgentPackageModeDefaultContext,
		Description: "Go package-focused agent with package-jail editing, testing, API analysis tools, and default initial context.",
		ToolNames: []string{
			coretools.ToolNameReadFile,
			coretools.ToolNameLS,
		},
		ToolsBuilder: buildPackageModeToolNames,
		SystemPromptBuilder: func(options agentregistry.BuildOptions) (string, error) {
			return buildPackageModeSystemPrompt(options, prompt.GoPackageModePromptKindFull)
		},
		InitialTurnsBuilder: buildPackageModeDefaultContextInitialTurns,
		AuthPolicy:          agentregistry.AuthPolicyPackage,
	}); err != nil {
		return nil, err
	}

	if err := registry.RegisterAgent(agentregistry.Definition{
		Name:        AgentLimitedPackageMode,
		Description: "Go package-focused agent for smaller mechanical package edits with default initial context and a limited toolset.",
		ToolNames: []string{
			coretools.ToolNameReadFile,
			coretools.ToolNameLS,
		},
		ToolsBuilder: buildLimitedPackageModeToolNames,
		SystemPromptBuilder: func(options agentregistry.BuildOptions) (string, error) {
			return buildPackageModeSystemPrompt(options, prompt.GoPackageModePromptKindUpdateUsage)
		},
		InitialTurnsBuilder: buildPackageModeDefaultContextInitialTurns,
		AuthPolicy:          agentregistry.AuthPolicyPackage,
	}); err != nil {
		return nil, err
	}

	if err := registry.RegisterAgent(agentregistry.Definition{
		Name:        agentClarifyPublicAPI,
		Description: "Read-only agent for clarifying public API docs for a single identifier.",
		ToolNames: []string{
			coretools.ToolNameReadFile,
			coretools.ToolNameLS,
		},
		SystemPromptBuilder: buildClarifyPublicAPISystemPrompt,
		InitialTurnsBuilder: buildClarifyPublicAPIInitialTurns,
	}); err != nil {
		return nil, err
	}

	if err := registry.ValidateTools(); err != nil {
		return nil, err
	}

	return registry, nil
}

func genericTools() map[string]toolsetinterface.Tool {
	return map[string]toolsetinterface.Tool{
		coretools.ToolNameApplyPatch: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			return coretools.NewApplyPatchTool(opts.Authorizer, true, packageModePostChecks(opts)), nil
		},
		coretools.ToolNameEdit: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			postChecks := packageModePostChecks(opts)
			if postChecks == nil {
				return coretools.NewEditTool(opts.Authorizer), nil
			}
			return coretools.NewEditTool(opts.Authorizer, postChecks), nil
		},
		coretools.ToolNameDelete: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			return coretools.NewDeleteTool(opts.Authorizer), nil
		},
		exttools.ToolNameDiagnostics: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			return exttools.NewDiagnosticsTool(opts.Authorizer), nil
		},
		pkgtools.ToolNameChangeAPI: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			return pkgtools.NewChangeAPITool(
				opts.GoPkgAbsDir,
				opts.Authorizer.WithoutCodeUnit(),
				changeAPIToolset(opts),
				opts.Model,
				opts.LintSteps,
				pkgtools.ChangeAPIToolOptions{AgentInvoker: opts.AgentInvoker},
			), nil
		},
		pkgtools.ToolNameClarifyPublicAPI: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			return pkgtools.NewClarifyPublicAPITool(
				opts.Authorizer.WithoutCodeUnit(),
				simpleReadOnlyTools,
				pkgtools.ClarifyPublicAPIToolOptions{
					AgentInvoker: opts.AgentInvoker,
					Model:        opts.Model,
				},
			), nil
		},
		coretools.ToolNameLS: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			return coretools.NewLsTool(opts.Authorizer), nil
		},
		exttools.ToolNameFixLints: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			return exttools.NewFixLintsTool(opts.Authorizer, opts.LintSteps), nil
		},
		pkgtools.ToolNameGetPublicAPI: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			return pkgtools.NewGetPublicAPITool(opts.Authorizer.WithoutCodeUnit()), nil
		},
		pkgtools.ToolNameGetUsage: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			return pkgtools.NewGetUsageTool(opts.Authorizer.WithoutCodeUnit()), nil
		},
		pkgtools.ToolNameModuleInfo: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			return pkgtools.NewModuleInfoTool(opts.Authorizer.WithoutCodeUnit()), nil
		},
		coretools.ToolNameReadFile: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			return coretools.NewReadFileTool(opts.Authorizer), nil
		},
		exttools.ToolNameRunProjectTests: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			return exttools.NewRunProjectTestsTool(opts.GoPkgAbsDir, opts.Authorizer.WithoutCodeUnit()), nil
		},
		exttools.ToolNameRunTests: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			return exttools.NewRunTestsTool(opts.Authorizer, opts.LintSteps), nil
		},
		coretools.ToolNameShell: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			return coretools.NewShellTool(opts.Authorizer), nil
		},
		coretools.ToolNameSkillShell: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			return coretools.NewSkillShellTool(opts.Authorizer), nil
		},
		coretools.ToolNameUpdatePlan: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			return coretools.NewUpdatePlanTool(opts.Authorizer), nil
		},
		pkgtools.ToolNameUpdateUsage: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			return pkgtools.NewUpdateUsageTool(
				opts.GoPkgAbsDir,
				opts.Authorizer.WithoutCodeUnit(),
				limitedPackageAgentTools,
				opts.Model,
				opts.LintSteps,
				pkgtools.UpdateUsageToolOptions{AgentInvoker: opts.AgentInvoker},
			), nil
		},
		coretools.ToolNameWrite: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			postChecks := packageModePostChecks(opts)
			if postChecks == nil {
				return coretools.NewWriteTool(opts.Authorizer), nil
			}
			return coretools.NewWriteTool(opts.Authorizer, postChecks), nil
		},
	}
}

func packageModePostChecks(opts toolsetinterface.Options) *coretools.ApplyPatchPostChecks {
	if !isPackageModeAgent(opts.AgentName) {
		return nil
	}
	lintSteps := opts.LintSteps
	if lintSteps == nil {
		lintSteps = lints.DefaultSteps()
	}
	return packagePostChecks(lintSteps)
}

func changeAPIToolset(opts toolsetinterface.Options) toolsetinterface.Toolset {
	if isFullPackageModeAgent(opts.AgentName) {
		return packageAgentTools
	}
	return limitedPackageAgentTools
}

func simpleReadOnlyTools(opts toolsetinterface.Options) ([]llmstream.Tool, error) {
	return buildTools(opts, []string{
		coretools.ToolNameReadFile,
		coretools.ToolNameLS,
	})
}

func packageAgentTools(opts toolsetinterface.Options) ([]llmstream.Tool, error) {
	opts.AgentName = AgentPackageModeNoContext
	toolNames := []string{
		coretools.ToolNameReadFile,
		coretools.ToolNameLS,
	}
	toolNames = append(toolNames, buildEditFileToolNames(opts.Model)...)
	toolNames = append(toolNames,
		coretools.ToolNameSkillShell,
		coretools.ToolNameUpdatePlan,
		exttools.ToolNameDiagnostics,
		exttools.ToolNameFixLints,
		exttools.ToolNameRunTests,
		exttools.ToolNameRunProjectTests,
		pkgtools.ToolNameModuleInfo,
		pkgtools.ToolNameGetPublicAPI,
		pkgtools.ToolNameClarifyPublicAPI,
		pkgtools.ToolNameGetUsage,
		pkgtools.ToolNameUpdateUsage,
		pkgtools.ToolNameChangeAPI,
	)
	return buildTools(opts, toolNames)
}

func limitedPackageAgentTools(opts toolsetinterface.Options) ([]llmstream.Tool, error) {
	opts.AgentName = AgentLimitedPackageMode
	toolNames := []string{
		coretools.ToolNameReadFile,
		coretools.ToolNameLS,
	}
	toolNames = append(toolNames, buildEditFileToolNames(opts.Model)...)
	toolNames = append(toolNames,
		coretools.ToolNameSkillShell,
		exttools.ToolNameDiagnostics,
		exttools.ToolNameFixLints,
		exttools.ToolNameRunTests,
		pkgtools.ToolNameGetPublicAPI,
		pkgtools.ToolNameClarifyPublicAPI,
	)
	return buildTools(opts, toolNames)
}

func buildTools(opts toolsetinterface.Options, toolNames []string) ([]llmstream.Tool, error) {
	builders := genericTools()
	tools := make([]llmstream.Tool, 0, len(toolNames))
	for _, toolName := range toolNames {
		builder, ok := builders[toolName]
		if !ok {
			return nil, fmt.Errorf("tool %q is not registered", toolName)
		}
		tool, err := builder(opts)
		if err != nil {
			return nil, fmt.Errorf("build tool %q: %w", toolName, err)
		}
		tools = append(tools, tool)
	}
	return tools, nil
}

func packagePostChecks(lintSteps []lints.Step) *coretools.ApplyPatchPostChecks {
	return &coretools.ApplyPatchPostChecks{
		RunDiagnostics: func(ctx context.Context, sandboxDir string, targetDir string) (string, error) {
			return exttools.RunDiagnostics(ctx, sandboxDir, postCheckTargetPath(sandboxDir, targetDir))
		},
		FixLints: func(ctx context.Context, sandboxDir string, targetDir string) (string, error) {
			return lints.Run(ctx, sandboxDir, postCheckTargetPath(sandboxDir, targetDir), lintSteps, lints.SituationPatch)
		},
	}
}

func postCheckTargetPath(sandboxDir string, targetDir string) string {
	if targetDir == "" {
		return sandboxDir
	}
	if filepath.IsAbs(targetDir) || sandboxDir == "" {
		return targetDir
	}
	return filepath.Join(sandboxDir, targetDir)
}

func isFullPackageModeAgent(agentName string) bool {
	switch agentName {
	case AgentPackageModeNoContext, AgentPackageModeDefaultContext:
		return true
	default:
		return false
	}
}

func isPackageModeAgent(agentName string) bool {
	switch agentName {
	case AgentPackageModeNoContext, AgentPackageModeDefaultContext, AgentLimitedPackageMode:
		return true
	default:
		return false
	}
}

func buildGenericToolNames(opts toolsetinterface.Options) ([]string, error) {
	toolNames := buildEditFileToolNames(opts.Model)
	toolNames = append(toolNames,
		coretools.ToolNameShell,
		coretools.ToolNameUpdatePlan,
	)
	return toolNames, nil
}

func buildPackageModeToolNames(opts toolsetinterface.Options) ([]string, error) {
	toolNames := buildEditFileToolNames(opts.Model)
	toolNames = append(toolNames,
		coretools.ToolNameSkillShell,
		coretools.ToolNameUpdatePlan,
		exttools.ToolNameDiagnostics,
		exttools.ToolNameFixLints,
		exttools.ToolNameRunTests,
		exttools.ToolNameRunProjectTests,
		pkgtools.ToolNameModuleInfo,
		pkgtools.ToolNameGetPublicAPI,
		pkgtools.ToolNameClarifyPublicAPI,
		pkgtools.ToolNameGetUsage,
		pkgtools.ToolNameUpdateUsage,
		pkgtools.ToolNameChangeAPI,
	)
	return toolNames, nil
}

func buildLimitedPackageModeToolNames(opts toolsetinterface.Options) ([]string, error) {
	toolNames := buildEditFileToolNames(opts.Model)
	toolNames = append(toolNames,
		coretools.ToolNameSkillShell,
		exttools.ToolNameDiagnostics,
		exttools.ToolNameFixLints,
		exttools.ToolNameRunTests,
		pkgtools.ToolNameGetPublicAPI,
		pkgtools.ToolNameClarifyPublicAPI,
	)
	return toolNames, nil
}

func buildEditFileToolNames(model llmmodel.ModelID) []string {
	if model.ProviderID() == llmmodel.ProviderIDOpenAI {
		return []string{coretools.ToolNameApplyPatch}
	}

	return []string{
		coretools.ToolNameEdit,
		coretools.ToolNameWrite,
		coretools.ToolNameDelete,
	}
}

func buildPackageModeSystemPrompt(options agentregistry.BuildOptions, promptKind prompt.GoPackageModePromptKind) (string, error) {
	systemPrompt := prompt.GetGoPackageModeModePrompt(promptKind)

	if err := skills.InstallDefault(); err != nil {
		return "", fmt.Errorf("install default skills: %w", err)
	}

	searchDir := options.ToolOptions.GoPkgAbsDir
	if strings.TrimSpace(searchDir) == "" {
		searchDir = options.ToolOptions.SandboxDir
	}

	validSkills, invalidSkills, failedSkillLoads, skillsErr := skills.LoadSkills(skills.SearchPaths(searchDir))
	if skillsErr != nil {
		validSkills = nil
		invalidSkills = nil
		failedSkillLoads = nil
	}
	_ = invalidSkills
	_ = failedSkillLoads

	if options.ToolOptions.Authorizer != nil {
		if err := skills.Authorize(validSkills, options.ToolOptions.Authorizer); err != nil {
			return "", fmt.Errorf("authorize skills: %w", err)
		}
	}

	return joinContextBlocks(
		systemPrompt,
		skills.Prompt(validSkills, coretools.ToolNameSkillShell, true),
	), nil
}

func buildPackageModeDefaultContextInitialTurns(ctx context.Context, options agentregistry.BuildOptions) ([]string, error) {
	sandboxAbsDir := strings.TrimSpace(options.ToolOptions.SandboxDir)
	if sandboxAbsDir == "" {
		return nil, errors.New("sandbox dir is required")
	}

	goPkgAbsDir := strings.TrimSpace(options.ToolOptions.GoPkgAbsDir)
	if goPkgAbsDir == "" {
		return nil, errors.New("go package dir is required")
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	lang, _ := detectlang.Detect(sandboxAbsDir, goPkgAbsDir)
	if lang != detectlang.LangGo {
		return nil, errors.New("only go is supported right now")
	}

	initialContext, err := buildGoPackageInitialContext(goPkgAbsDir, options.ToolOptions.LintSteps)
	if err != nil {
		return nil, err
	}

	return []string{
		buildClarifyPublicAPIEnvTurn(sandboxAbsDir),
		initialContext,
	}, nil
}

func buildGoPackageInitialContext(goPkgAbsDir string, lintSteps []lints.Step) (string, error) {
	module, err := gocode.NewModule(goPkgAbsDir)
	if err != nil {
		return "", err
	}

	relDir, err := filepath.Rel(module.AbsolutePath, goPkgAbsDir)
	if err != nil {
		return "", fmt.Errorf("determine package relative dir: %w", err)
	}
	relDir = normalizeModuleRelativeDir(relDir)

	pkg, err := module.LoadPackageByRelativeDir(relDir)
	if err != nil {
		return "", err
	}

	initial, err := initialcontext.Create(pkg, lintSteps, false)
	if err != nil {
		return "", fmt.Errorf("initial context: %w", err)
	}

	return initial, nil
}

func joinContextBlocks(blocks ...string) string {
	nonEmpty := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if strings.TrimSpace(block) == "" {
			continue
		}
		nonEmpty = append(nonEmpty, strings.TrimSpace(block))
	}
	return strings.Join(nonEmpty, "\n\n")
}

func buildClarifyPublicAPISystemPrompt(options agentregistry.BuildOptions) (string, error) {
	var builder strings.Builder
	builder.WriteString(prompt.GetBasicPrompt())
	builder.WriteString("\n\nYou are a read-only agent for clarifying public API documentation for a single identifier.\n")
	builder.WriteString("Use the initial context and available tools (`ls`, `read_file`) to answer the user's question.\n")
	builder.WriteString("If information is missing or the identifier cannot be found, clearly say so and explain what would be needed.\n")
	builder.WriteString("Respond concisely and directly. The questioner cannot see non-exported implementation details, so ground your answer in the docs, files, and context you can read.")
	return builder.String(), nil
}

func buildClarifyPublicAPIInitialTurns(ctx context.Context, options agentregistry.BuildOptions) ([]string, error) {
	request, err := parseClarifyPublicAPIRequest(options.Request.Messages)
	if err != nil {
		return nil, err
	}

	absPath, initialContext, err := buildClarifyPublicAPIContext(ctx, options.ToolOptions.SandboxDir, request.Path, request.Identifier)
	if err != nil {
		return nil, err
	}

	return []string{
		buildClarifyPublicAPIEnvTurn(options.ToolOptions.SandboxDir),
		buildClarifyPublicAPIContextTurn(absPath, request.Identifier, initialContext),
	}, nil
}

func parseClarifyPublicAPIRequest(messages []string) (clarifyPublicAPIRequest, error) {
	if len(messages) == 0 {
		return clarifyPublicAPIRequest{}, errors.New("clarify_public_api agent requires a request message")
	}

	raw := messages[0]
	var request clarifyPublicAPIRequest
	if err := json.Unmarshal([]byte(raw), &request); err == nil {
		if validateClarifyPublicAPIRequest(request) == nil {
			return request, nil
		}
	}

	request = parseClarifyPublicAPITextRequest(raw)
	if err := validateClarifyPublicAPIRequest(request); err != nil {
		return clarifyPublicAPIRequest{}, err
	}

	return request, nil
}

func parseClarifyPublicAPITextRequest(raw string) clarifyPublicAPIRequest {
	var request clarifyPublicAPIRequest

	lines := strings.Split(raw, "\n")
	questionLine := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "Path:"):
			request.Path = strings.TrimSpace(strings.TrimPrefix(trimmed, "Path:"))
		case strings.HasPrefix(trimmed, "Identifier:"):
			request.Identifier = strings.TrimSpace(strings.TrimPrefix(trimmed, "Identifier:"))
		case strings.HasPrefix(trimmed, "Question:"):
			questionLine = i
		}
	}

	if questionLine == -1 {
		return request
	}

	questionLines := append([]string(nil), lines[questionLine:]...)
	questionLines[0] = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(questionLines[0]), "Question:"))
	request.Question = strings.TrimSpace(strings.Join(questionLines, "\n"))

	return request
}

func validateClarifyPublicAPIRequest(request clarifyPublicAPIRequest) error {
	if strings.TrimSpace(request.Path) == "" {
		return errors.New("clarify_public_api request path is required")
	}
	if strings.TrimSpace(request.Identifier) == "" {
		return errors.New("clarify_public_api request identifier is required")
	}
	if strings.TrimSpace(request.Question) == "" {
		return errors.New("clarify_public_api request question is required")
	}
	return nil
}

func buildClarifyPublicAPIContext(ctx context.Context, sandboxAbsDir string, path string, identifier string) (string, string, error) {
	absPath, stat, err := normalizeClarifyPublicAPIPath(sandboxAbsDir, path)
	if err != nil {
		return "", "", err
	}
	if err := ctx.Err(); err != nil {
		return "", "", err
	}

	lang, _ := detectlang.Detect(sandboxAbsDir, absPath)
	if lang == detectlang.LangGo {
		initial, ok, err := tryBuildClarifyGoContext(absPath)
		if err != nil {
			return "", "", err
		}
		if ok {
			return absPath, initial, nil
		}
	}

	initial, err := buildClarifyGenericContext(absPath, stat, identifier)
	if err != nil {
		return "", "", err
	}

	return absPath, initial, nil
}

func normalizeClarifyPublicAPIPath(sandboxAbsDir string, path string) (string, os.FileInfo, error) {
	if strings.TrimSpace(sandboxAbsDir) == "" {
		return "", nil, errors.New("sandbox dir is required")
	}

	if !filepath.IsAbs(sandboxAbsDir) {
		absSandbox, err := filepath.Abs(sandboxAbsDir)
		if err != nil {
			return "", nil, fmt.Errorf("make sandbox dir absolute: %w", err)
		}
		sandboxAbsDir = absSandbox
	}

	sandboxInfo, err := os.Stat(sandboxAbsDir)
	if err != nil {
		return "", nil, fmt.Errorf("stat sandbox dir %q: %w", sandboxAbsDir, err)
	}
	if !sandboxInfo.IsDir() {
		return "", nil, fmt.Errorf("sandbox dir %q is not a directory", sandboxAbsDir)
	}

	absPath, relPath, err := coretools.NormalizePath(path, sandboxAbsDir, coretools.WantPathTypeAny, true)
	if err != nil {
		return "", nil, fmt.Errorf("normalize path: %w", err)
	}
	if relPath == "" {
		return "", nil, fmt.Errorf("path %q is outside of sandbox %q", path, sandboxAbsDir)
	}

	sandboxRealAbsDir, err := filepath.EvalSymlinks(sandboxAbsDir)
	if err != nil {
		return "", nil, fmt.Errorf("resolve sandbox dir symlinks %q: %w", sandboxAbsDir, err)
	}
	absRealPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return "", nil, fmt.Errorf("resolve path symlinks %q: %w", absPath, err)
	}
	if !clarifyPathWithinDir(sandboxRealAbsDir, absRealPath) {
		return "", nil, fmt.Errorf("path %q is outside of sandbox %q", path, sandboxAbsDir)
	}

	stat, err := os.Stat(absPath)
	if err != nil {
		return "", nil, fmt.Errorf("stat path %q: %w", absPath, err)
	}

	return absPath, stat, nil
}

func buildClarifyPublicAPIEnvTurn(sandboxAbsDir string) string {
	return "<env>\nSandbox directory: " + sandboxAbsDir + "\n</env>"
}

func buildClarifyPublicAPIContextTurn(absPath string, identifier string, initialContext string) string {
	var builder strings.Builder
	builder.WriteString("Clarification target:\n")
	builder.WriteString("Identifier: ")
	builder.WriteString(identifier)
	builder.WriteString("\nPath: ")
	builder.WriteString(absPath)
	builder.WriteString("\n\n")
	if initialContext == "" {
		builder.WriteString("No initial context was precomputed. Use the available tools if more detail is needed.")
		return builder.String()
	}

	builder.WriteString("Initial context:\n")
	builder.WriteString(initialContext)
	return builder.String()
}

func tryBuildClarifyGoContext(absPath string) (string, bool, error) {
	module, err := gocode.NewModule(absPath)
	if err != nil {
		return "", false, nil
	}

	stat, err := os.Stat(absPath)
	if err != nil {
		return "", false, fmt.Errorf("stat path for Go context: %w", err)
	}

	pkgDir := absPath
	if !stat.IsDir() {
		pkgDir = filepath.Dir(absPath)
	}

	relDir, err := filepath.Rel(module.AbsolutePath, pkgDir)
	if err != nil {
		return "", false, fmt.Errorf("determine package relative dir: %w", err)
	}
	relDir = normalizeModuleRelativeDir(relDir)

	pkg, err := module.LoadPackageByRelativeDir(relDir)
	if err != nil {
		return "", false, nil
	}

	initial, err := initialcontext.Create(pkg, nil, true)
	if err != nil {
		return "", false, fmt.Errorf("initial context: %w", err)
	}

	return initial, true, nil
}

func normalizeModuleRelativeDir(relDir string) string {
	relDir = filepath.ToSlash(relDir)
	if relDir == "." {
		return ""
	}
	return relDir
}

func buildClarifyGenericContext(absPath string, stat os.FileInfo, identifier string) (string, error) {
	dir := absPath
	target := "."
	if !stat.IsDir() {
		dir = filepath.Dir(absPath)
		target = filepath.Base(absPath)
	}

	return runClarifyRipgrep(dir, target, identifier), nil
}

func runClarifyRipgrep(cwd string, target string, identifier string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	runner := cmdrunner.NewRunner(nil, nil)
	runner.AddCommand(cmdrunner.Command{
		Command: "rg",
		Args: []string{
			"--line-number",
			"--color=never",
			"-C", clarifyRGLines,
			identifier,
			target,
		},
		CWD:     cwd,
		ShowCWD: true,
	})

	result, err := runner.Run(ctx, cwd, nil)
	if err != nil {
		return fmt.Sprintf("Failed to run ripgrep: %v", err)
	}

	return result.ToXML("ripgrep")
}

func clarifyPathWithinDir(rootAbsDir string, targetAbsPath string) bool {
	if rootAbsDir == "" || targetAbsPath == "" {
		return false
	}

	rel, err := filepath.Rel(rootAbsDir, targetAbsPath)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	if rel == ".." {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}
