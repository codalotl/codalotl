package agentbuilder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/codalotl/codalotl/internal/agentregistry"
	"github.com/codalotl/codalotl/internal/agentsmd"
	"github.com/codalotl/codalotl/internal/detectlang"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/initialcontext"
	"github.com/codalotl/codalotl/internal/lints"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/prompt"
	"github.com/codalotl/codalotl/internal/q/cmdrunner"
	"github.com/codalotl/codalotl/internal/tools/coretools"
	"github.com/codalotl/codalotl/internal/tools/exttools"
	"github.com/codalotl/codalotl/internal/tools/pkgtools"
	"github.com/codalotl/codalotl/internal/tools/spectools"
	"github.com/codalotl/codalotl/internal/tools/toolsetinterface"
)

// Built-in agent names recognized by agentbuilder.
const (
	// AgentGeneric is the registry name for the general-purpose agent.
	AgentGeneric string = "generic"

	// AgentPackageModeNoContext is the registry name for the full package-mode agent without precomputed package context.
	AgentPackageModeNoContext string = "package_mode_no_context"

	AgentPackageModeDefaultContext string = "package_mode_default_context" // AgentPackageModeDefaultContext adds package initial context before user messages.

	// AgentLimitedPackageMode is the registry name for the limited package-mode agent for targeted package work.
	AgentLimitedPackageMode string = "limited_package_mode"

	agentImprovePublicAPIDocs = "improve_public_api_docs"
	agentClarifyPublicAPI     = pkgtools.ToolNameClarifyPublicAPI
	clarifyRGLines            = "4"
)

// clarifyPublicAPIRequest describes one request for the clarify_public_api agent.
type clarifyPublicAPIRequest struct {
	Path       string `json:"path"`       // Path is the target source path used to build API context.
	Identifier string `json:"identifier"` // Identifier is the public API symbol to clarify.
	Question   string `json:"question"`   // Question is the user question about the identifier.
}

var (
	toolOverridesMu sync.RWMutex
	toolOverrides   = map[string]toolsetinterface.Tool{}
)

// OverrideTool registers or replaces a named tool builder for future BuildRegistry calls.
//
// Process-wide startup configuration for optional YAML-listed tools such as `codalotl_cli` and `refactor`.
func OverrideTool(toolName string, tool toolsetinterface.Tool) {
	if toolName == "" {
		panic("agentbuilder.OverrideTool: toolName is required")
	}
	if tool == nil {
		panic("agentbuilder.OverrideTool: tool is required")
	}

	toolOverridesMu.Lock()
	defer toolOverridesMu.Unlock()

	toolOverrides[toolName] = tool
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

	if err := addEmbeddedYAMLToRegistry(registry); err != nil {
		return nil, err
	}

	if err := registry.ValidateTools(); err != nil {
		return nil, err
	}

	return registry, nil
}

func genericTools() map[string]toolsetinterface.Tool {
	builders := builtinTools()

	toolOverridesMu.RLock()
	defer toolOverridesMu.RUnlock()

	for toolName, tool := range toolOverrides {
		builders[toolName] = tool
	}

	return builders
}

// builtinTools returns a new map of built-in tool builders keyed by tool name. Each builder constructs its tool from the supplied toolset options, including package-mode
// post-checks and subagent options where applicable.
func builtinTools() map[string]toolsetinterface.Tool {
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
					LintSteps:    opts.LintSteps,
				},
			), nil
		},
		coretools.ToolNameLS: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			return coretools.NewLsTool(opts.Authorizer), nil
		},
		exttools.ToolNameFixLints: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			return exttools.NewFixLintsTool(opts.Authorizer, opts.LintSteps), nil
		},
		spectools.ToolNameCheckSpecConformance: func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			return spectools.NewCheckSpecConformanceTool(
				opts.Authorizer.WithoutCodeUnit(),
				spectools.CheckSpecConformanceToolOptions{
					AgentInvoker: opts.AgentInvoker,
					Model:        opts.Model,
				},
			), nil
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

// packageAgentTools builds the full package-mode toolset for package-scoped agents.
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

// The limitedPackageAgentTools function builds the limited package-mode toolset for targeted package work.
//
// It sets the effective agent name to AgentLimitedPackageMode and includes file reading, listing, model-specific edit tools, skill shell, diagnostics, lint fixing,
// tests, and public API inspection tools.
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
	case AgentPackageModeNoContext, AgentPackageModeDefaultContext, AgentLimitedPackageMode, agentImprovePublicAPIDocs:
		return true
	default:
		return false
	}
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
	return buildSkillsEnabledSystemPrompt(
		options,
		prompt.GetGoPackageModeModePrompt(promptKind),
		coretools.ToolNameSkillShell,
		true,
	)
}

func buildGenericAgentsMDInitialTurns(ctx context.Context, options agentregistry.BuildOptions) ([]string, error) {
	sandboxAbsDir := strings.TrimSpace(options.ToolOptions.SandboxDir)
	if sandboxAbsDir == "" {
		return nil, errors.New("sandbox dir is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	agentsTurn, err := buildAgentsMDInitialTurn(sandboxAbsDir, sandboxAbsDir)
	if err != nil {
		return nil, err
	}
	if agentsTurn == "" {
		return nil, nil
	}

	return []string{agentsTurn}, nil
}

// buildPackageModeAgentsMDInitialTurns returns an AGENTS.md initial turn for a package-mode agent.
func buildPackageModeAgentsMDInitialTurns(ctx context.Context, options agentregistry.BuildOptions) ([]string, error) {
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

	agentsTurn, err := buildAgentsMDInitialTurn(sandboxAbsDir, goPkgAbsDir)
	if err != nil {
		return nil, err
	}
	if agentsTurn == "" {
		return nil, nil
	}

	return []string{agentsTurn}, nil
}

func buildPackageModeDefaultContextInitialTurns(ctx context.Context, options agentregistry.BuildOptions) ([]string, error) {
	return buildPackageModeContextInitialTurns(ctx, options, true)
}

// buildPackageModeContextInitialTurns builds the ordered initial user turns for a package-mode agent. It requires SandboxDir and GoPkgAbsDir in options.ToolOptions,
// always includes the sandbox environment turn, optionally includes AGENTS.md when available, and appends package context built with the configured lint steps.
func buildPackageModeContextInitialTurns(ctx context.Context, options agentregistry.BuildOptions, includeAgentsMD bool) ([]string, error) {
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

	turns := []string{buildClarifyPublicAPIEnvTurn(sandboxAbsDir)}
	if includeAgentsMD {
		agentsTurn, err := buildAgentsMDInitialTurn(sandboxAbsDir, goPkgAbsDir)
		if err != nil {
			return nil, err
		}
		if agentsTurn != "" {
			turns = append(turns, agentsTurn)
		}
	}

	initialContext, err := buildPackageModeInitialContextTurn(ctx, sandboxAbsDir, goPkgAbsDir, options.ToolOptions.LintSteps)
	if err != nil {
		return nil, err
	}
	turns = append(turns, initialContext)

	return turns, nil
}

// buildPackageModeInitialContextTurn builds the initial context turn for a package-mode invocation of goPkgAbsDir. It observes ctx cancellation, uses lintSteps
// in the generated Go package context, and falls back to module-level context when the directory cannot be loaded as a package.
func buildPackageModeInitialContextTurn(ctx context.Context, sandboxAbsDir string, goPkgAbsDir string, lintSteps []lints.Step) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	lang, _ := detectlang.Detect(sandboxAbsDir, goPkgAbsDir)
	initialContext, err := buildGoPackageInitialContext(goPkgAbsDir, lintSteps)
	if err == nil {
		return initialContext, nil
	}

	fallbackContext, fallbackOK, fallbackErr := tryBuildGoPackageFallbackInitialContext(goPkgAbsDir, err)
	if fallbackErr != nil {
		return "", fallbackErr
	}
	if fallbackOK {
		return fallbackContext, nil
	}
	if lang != detectlang.LangGo {
		return "", errors.New("only go is supported right now")
	}
	return "", err
}

func buildAgentsMDInitialTurn(sandboxAbsDir string, cwd string) (string, error) {
	if strings.TrimSpace(sandboxAbsDir) == "" {
		return "", errors.New("sandbox dir is required")
	}
	if strings.TrimSpace(cwd) == "" {
		return "", errors.New("cwd is required")
	}

	text, err := agentsmd.Read(sandboxAbsDir, cwd)
	if err != nil {
		// Keep AGENTS.md injection best-effort so a bad or unreadable file does not block agent startup.
		return "", nil
	}
	return strings.TrimSpace(text), nil
}

// buildGoPackageInitialContext returns the initial LLM context for a Go package directory.
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

// tryBuildGoPackageFallbackInitialContext attempts to build package-mode context for a directory that could not be loaded as a Go package. It returns ok false without
// error when goPkgAbsDir does not exist, is not a directory, or is not inside a Go module. When it succeeds, the context includes module and package identity, a
// shallow directory listing, and loadErr as the package-load diagnostic.
func tryBuildGoPackageFallbackInitialContext(goPkgAbsDir string, loadErr error) (string, bool, error) {
	if strings.TrimSpace(goPkgAbsDir) == "" {
		return "", false, errors.New("go package dir is required")
	}

	info, err := os.Stat(goPkgAbsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("stat package dir %q: %w", goPkgAbsDir, err)
	}
	if !info.IsDir() {
		return "", false, nil
	}

	module, err := gocode.NewModule(goPkgAbsDir)
	if err != nil {
		return "", false, nil
	}

	relDir, err := filepath.Rel(module.AbsolutePath, goPkgAbsDir)
	if err != nil {
		return "", false, fmt.Errorf("determine package relative dir: %w", err)
	}
	relDir = normalizeModuleRelativeDir(relDir)

	dirListing, err := buildFallbackPackageDirListing(goPkgAbsDir)
	if err != nil {
		return "", false, err
	}

	importPath := module.Name
	if relDir != "" {
		if importPath == "" {
			importPath = relDir
		} else {
			importPath += "/" + relDir
		}
	}

	return buildFallbackPackageInitialContext(module.Name, relDir, goPkgAbsDir, importPath, dirListing, loadErr), true, nil
}

// The buildFallbackPackageDirListing function returns an XML-style listing of the non-hidden entries in a Go package directory.
//
// The listing is filename-sorted, non-recursive, uses ls -1p formatting, appends "/" to directories, and includes goPkgAbsDir as the cwd. It returns an error if
// the directory cannot be read.
func buildFallbackPackageDirListing(goPkgAbsDir string) (string, error) {
	entries, err := os.ReadDir(goPkgAbsDir)
	if err != nil {
		return "", fmt.Errorf("read package dir %q: %w", goPkgAbsDir, err)
	}

	var builder strings.Builder
	fmt.Fprintf(&builder, "<ls ok=\"true\" cwd=%q>\n", goPkgAbsDir)
	builder.WriteString("$ ls -1p\n")
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		builder.WriteString(name)
		if entry.IsDir() {
			builder.WriteString("/")
		}
		builder.WriteString("\n")
	}
	builder.WriteString("</ls>")
	return builder.String(), nil
}

// buildFallbackPackageInitialContext builds initial package-mode context for a directory that cannot yet be loaded as a Go package. The context includes package
// identity, the supplied directory listing, and placeholder package, diagnostics, test, lint, and used-by sections that explain the load failure.
func buildFallbackPackageInitialContext(modulePath string, relDir string, goPkgAbsDir string, importPath string, dirListing string, loadErr error) string {
	loadMessage := "target directory does not currently load as a Go package"
	if loadErr != nil {
		loadMessage += ": " + strings.TrimSpace(loadErr.Error())
	}

	var currentPackage strings.Builder
	currentPackage.WriteString("<current-package>\n")
	fmt.Fprintf(&currentPackage, "Module path: %q\n", modulePath)
	fmt.Fprintf(&currentPackage, "Package relative path: %q\n", relDir)
	fmt.Fprintf(&currentPackage, "Absolute package path: %q\n", goPkgAbsDir)
	fmt.Fprintf(&currentPackage, "Package import path: %q\n", importPath)
	currentPackage.WriteString("</current-package>")

	return joinContextBlocks(
		currentPackage.String(),
		dirListing,
		"<pkg-map type=\"non-tests\">\n(fallback package context; "+loadMessage+")\n</pkg-map>",
		"<pkg-map type=\"tests\">\n(no test package data available yet)\n</pkg-map>",
		"<used-by>\n(used-by data unavailable until the directory loads as a Go package)\n</used-by>",
		"<diagnostics-status ok=\"unknown\">\n(diagnostics not run; "+loadMessage+")\n</diagnostics-status>",
		"<test-status ok=\"unknown\">\n(tests not run; "+loadMessage+")\n</test-status>",
		"<lint-status ok=\"unknown\">\n(lints not run; "+loadMessage+")\n</lint-status>",
	)
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
	request, err := parseClarifyPublicAPIRequest(options.Request.Payload, options.Request.Messages)
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

// parseClarifyPublicAPIRequest parses and validates a clarify_public_api request from a payload or messages.
//
// If payload is non-empty, it must contain a JSON request. Otherwise, the first message may contain either JSON or a text request with Path, Identifier, and Question
// labels.
func parseClarifyPublicAPIRequest(payload json.RawMessage, messages []string) (clarifyPublicAPIRequest, error) {
	if len(payload) > 0 {
		var request clarifyPublicAPIRequest
		if err := json.Unmarshal(payload, &request); err != nil {
			return clarifyPublicAPIRequest{}, fmt.Errorf("clarify_public_api request payload: %w", err)
		}
		if err := validateClarifyPublicAPIRequest(request); err != nil {
			return clarifyPublicAPIRequest{}, err
		}
		return request, nil
	}

	if len(messages) == 0 {
		return clarifyPublicAPIRequest{}, errors.New("clarify_public_api agent requires a request payload")
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

// parseClarifyPublicAPITextRequest parses a labeled text clarify_public_api request.
//
// The text form uses Path, Identifier, and Question labels. The question includes the text after the Question label and any following lines; missing fields are
// left empty for caller validation.
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

// buildClarifyPublicAPIContext builds the initial context for a clarify_public_api request. It resolves path inside sandboxAbsDir and returns the resolved absolute
// path with context text. Go paths use package context when available; other paths, and Go paths without package context, use generic search context for identifier.
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

// normalizeClarifyPublicAPIPath resolves path to an existing file or directory inside sandboxAbsDir and returns its absolute path and file information. sandboxAbsDir
// may be absolute or relative but must name an existing directory. The function rejects paths that leave the sandbox, including through symlinks.
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

// tryBuildClarifyGoContext returns Go package context for a clarification path when it can be built.
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

// The runClarifyRipgrep function searches target for identifier with ripgrep and returns the result as ripgrep XML.
//
// It runs in cwd with line numbers, no color, clarifyRGLines lines of context, and a 10-second timeout. If ripgrep cannot be run or times out, it returns a textual
// failure message.
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
