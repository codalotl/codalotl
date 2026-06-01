package spectools

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/codeunit"
	textdiff "github.com/codalotl/codalotl/internal/diff"
	"github.com/codalotl/codalotl/internal/gittools"
	"github.com/codalotl/codalotl/internal/gocas"
	"github.com/codalotl/codalotl/internal/gocas/casconformance"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/specmd"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/codalotl/codalotl/internal/tools/coretools"
	"github.com/codalotl/codalotl/internal/tools/toolsetinterface"
)

//go:embed check_spec_conformance.md
var descriptionCheckSpecConformance string

// ToolNameCheckSpecConformance is the tool name used to register and invoke SPEC.md conformance checks.
const ToolNameCheckSpecConformance = "check_spec_conformance"

const defaultMaxConcurrency = 5

// This mirrors internal/agentbuilder.AgentLimitedPackageMode without importing that package and creating an import cycle.
const checkSpecConformanceAgentName = "limited_package_mode"

// CheckSpecConformanceToolOptions configures NewCheckSpecConformanceTool.
type CheckSpecConformanceToolOptions struct {
	AgentInvoker   toolsetinterface.AgentInvoker // AgentInvoker creates package-check subagents. If nil, package checks fail with an agent-unavailable error.
	Model          llmmodel.ModelID              // Model is passed to package-check subagents as their requested model.
	MaxConcurrency int                           // 0: use default concurrency
}

// toolCheckSpecConformance implements the check_spec_conformance tool for the current Go module.
type toolCheckSpecConformance struct {
	sandboxAbsDir   string                                                      // sandboxAbsDir is the absolute sandbox root used to load the module and configure subagents.
	authorizer      authdomain.Authorizer                                       // authorizer authorizes module reads and CAS writes; nil disables authorization checks.
	agentInvoker    toolsetinterface.AgentInvoker                               // agentInvoker creates package-check subagents.
	model           llmmodel.ModelID                                            // model is the model passed to package-check subagents.
	maxConcurrency  int                                                         // maxConcurrency bounds concurrent package checks; values less than one use the default.
	git             gitRunner                                                   // git runs git commands used for branch, diff, and untracked-file discovery.
	specDiffContext func(pkg *gocode.Package) (string, error)                   // specDiffContext computes extra SPEC diff context for a package; nil omits it.
	heuristicBase   func(repoDir string) (commit string, ref string, err error) // heuristicBase selects the comparison base commit and parent ref.

	// changedPaths returns repository paths changed since the comparison base.
	changedPaths func(repoDir string, baseCommit string, includeUncommitted bool) ([]string, error)

	// runPackageCheck checks one package and returns its final JSON verdict.
	runPackageCheck packageCheckRunner

	// subAgentCreatorFromContext returns the active tool-call subagent creator; nil uses the default.
	subAgentCreatorFromContext func(ctx context.Context) (agent.SubAgentCreator, error)

	// runAgentTurn sends one prompt to a package-check subagent and returns its answer; nil uses the default.
	runAgentTurn func(ctx context.Context, agent *agent.Agent, message string) (string, error)
}

// The checkSpecConformanceParams type is the JSON request payload for check_spec_conformance.
type checkSpecConformanceParams struct {
	OnlyChanged bool     `json:"only_changed"` // OnlyChanged restricts checks to packages with package-scoped changes since the comparison base.
	Packages    []string `json:"packages"`     // Packages optionally names explicit packages by current-module import path or module-relative package path.
}

// The comparisonBase type identifies the git state used as the diff baseline for a conformance run.
type comparisonBase struct {
	Branch       string // Branch is the current branch being checked.
	ParentBranch string // ParentBranch is the heuristic parent branch for Branch, when one is known.
	Commit       string // Commit is the comparison-base commit hash.
}

// The repoChanges type records changed repository paths split by git tracking state.
type repoChanges struct {
	tracked   []string // Tracked contains changed tracked paths relative to the repository root.
	untracked []string // Untracked contains changed untracked paths relative to the repository root.
}

// The packagePathScope type defines the package boundary used to attribute changed paths.
type packagePathScope struct {
	codeUnit        *codeunit.CodeUnit // CodeUnit classifies existing files that belong to the package.
	moduleAbsDir    string             // ModuleAbsDir is the absolute module root used to resolve relative paths.
	packageRelDir   string             // PackageRelDir is the normalized module-relative package directory, or "" for the module root.
	blockedSubtrees []string           // BlockedSubtrees contains module-relative descendant package and hidden directories excluded from missing-path attribution.
}

// The eligiblePackage type records a package selected for SPEC.md conformance checking.
type eligiblePackage struct {
	Key     string          // Key is the module-relative package key used in the result map.
	Package *gocode.Package // Package is the Go package selected for checking.
	HasDiff bool            // HasDiff reports whether the package scope changed relative to the comparison base.
}

// The packageCheckRequest type carries package-specific context for a SPEC.md conformance check.
type packageCheckRequest struct {
	Key            string          // Key is the module-relative package key used in tool results and subagent labels.
	Package        *gocode.Package // Package is the Go package being checked.
	HasDiff        bool            // HasDiff reports whether the package scope changed relative to ComparisonBase.
	PackageDiff    string          // PackageDiff is the package diff against the comparison-base commit.
	SpecDiff       string          // SpecDiff is the precomputed SPEC.md diff context for the package.
	ComparisonBase comparisonBase  // ComparisonBase identifies the git state used to produce PackageDiff.
}

// packageCheckRunner checks one package for SPEC.md conformance and returns its JSON result. A non-nil error indicates that no valid package verdict was produced.
type packageCheckRunner func(ctx context.Context, req packageCheckRequest) (string, error)

// The packageCheckResult type represents one package's machine-readable SPEC.md conformance result.
type packageCheckResult struct {
	// Conforms is true for conforming packages, false for nonconforming packages, and nil only when Error is set.
	Conforms *bool `json:"conforms,omitempty"`

	// Nonconformances lists one or more SPEC.md issues when Conforms is false and must be empty when Conforms is true.
	Nonconformances []packageIssue `json:"nonconformances,omitempty"`

	// Error reports a package-scoped failure before a valid verdict was produced.
	Error string `json:"error,omitempty"`

	// PostcheckError reports package-scoped work that failed after a valid verdict was produced.
	PostcheckError string `json:"postcheck_error,omitempty"`
}

// The packageIssue type describes one SPEC.md nonconformance in a package-check result.
type packageIssue struct {
	Severity string `json:"severity"`           // Severity is "trivial", "minor", or "major".
	Latent   bool   `json:"latent"`             // Latent is true when the issue predates the comparison base.
	Message  string `json:"message"`            // Message is the human-readable nonconformance explanation.
	Analysis string `json:"analysis,omitempty"` // Analysis explains fix-vs-spec-update considerations for orchestrators.
}

// CheckSpecConformancePackageResult is one package entry in a check_spec_conformance tool result.
type CheckSpecConformancePackageResult = packageCheckResult

// CheckSpecConformanceIssue is one reported SPEC.md nonconformance.
type CheckSpecConformanceIssue = packageIssue

// CheckSpecConformanceResults is the parsed raw JSON result of check_spec_conformance.
type CheckSpecConformanceResults map[string]CheckSpecConformancePackageResult

// CheckSpecConformanceSummary groups package result counts and sorted package keys.
type CheckSpecConformanceSummary struct {
	ConformingCount       int                                  // ConformingCount is len(ConformingPackages).
	NonconformingCount    int                                  // NonconformingCount is len(NonconformingPackages).
	ErrorCount            int                                  // ErrorCount is len(ErrorPackages) and does not count PostcheckErrors.
	ConformingPackages    []string                             // ConformingPackages contains sorted package keys with valid conforming verdicts.
	NonconformingPackages []string                             // NonconformingPackages contains sorted package keys with valid nonconforming verdicts.
	ErrorPackages         []string                             // ErrorPackages contains sorted package keys whose checks failed before producing valid verdicts.
	PostcheckErrors       []CheckSpecConformancePostcheckError // PostcheckErrors contains package-scoped failures that happened after valid verdicts.
}

// CheckSpecConformancePostcheckError is a package-scoped failure that happened after a valid verdict.
type CheckSpecConformancePostcheckError struct {
	Package string // Package is the module-relative package key associated with the failure.
	Error   string // Error is the human-readable postcheck failure message.
}

// The packageResultValidationOptions type controls package-check result validation.
type packageResultValidationOptions struct {
	allowError          bool  // AllowError permits an error-only package result instead of a verdict.
	allowPostcheckError bool  // AllowPostcheckError permits postcheck_error to accompany a valid verdict.
	requireAnalysis     bool  // RequireAnalysis requires every nonconformance to include analysis text.
	hasDiff             *bool // HasDiff, when non-nil, reports package diff presence and marks all issues latent when false.
}

// The gitRunner interface runs git commands in a repository.
type gitRunner interface {
	// Output runs git with args in repoAbsDir and returns command output.
	Output(ctx context.Context, repoAbsDir string, args ...string) (string, error)
}

// execGitRunner runs Git commands through the local git executable. The zero value is ready to use.
type execGitRunner struct{}

// NewCheckSpecConformanceTool creates a tool that checks SPEC.md conformance for packages in the current module and records conforming packages in CAS.
//
// authorizer should be a sandbox authorizer, not a package-jail authorizer.
func NewCheckSpecConformanceTool(authorizer authdomain.Authorizer, options ...CheckSpecConformanceToolOptions) llmstream.Tool {
	var option CheckSpecConformanceToolOptions
	if len(options) > 0 {
		option = options[0]
	}

	sandboxAbsDir := ""
	if authorizer != nil {
		sandboxAbsDir = authorizer.SandboxDir()
	}

	tool := &toolCheckSpecConformance{
		sandboxAbsDir:              sandboxAbsDir,
		authorizer:                 authorizer,
		agentInvoker:               option.AgentInvoker,
		model:                      option.Model,
		maxConcurrency:             option.MaxConcurrency,
		git:                        execGitRunner{},
		specDiffContext:            computeSpecDiffContext,
		heuristicBase:              gittools.HeuristicMergeBase,
		changedPaths:               gittools.ChangedPathsSince,
		subAgentCreatorFromContext: subAgentCreatorFromContextSafe,
		runAgentTurn:               runPackageCheckAgentTurn,
	}
	tool.runPackageCheck = tool.runPackageCheckWithSubagent
	return tool
}

// Name returns the registered tool name for SPEC.md conformance checks.
func (t *toolCheckSpecConformance) Name() string {
	return ToolNameCheckSpecConformance
}

// Presenter returns the semantic presenter for check_spec_conformance tool calls and results.
func (t *toolCheckSpecConformance) Presenter() llmstream.Presenter {
	return checkSpecConformancePresenterInstance
}

// Info returns the LLM-facing registration metadata for the check_spec_conformance tool. The metadata declares only_changed as required and packages as an optional
// list of current-module import paths or module-relative package paths.
func (t *toolCheckSpecConformance) Info() llmstream.ToolInfo {
	return llmstream.ToolInfo{
		Name:        ToolNameCheckSpecConformance,
		Description: strings.TrimSpace(descriptionCheckSpecConformance),
		Parameters: map[string]any{
			"only_changed": map[string]any{
				"type":        "boolean",
				"description": "If true, only check packages whose state changed (in feature branches, compares on-disk state to branch merge base).",
			},
			"packages": map[string]any{
				"type":        "array",
				"description": "Optional package list. Entries may be current-module import paths or module-relative package paths.",
				"items": map[string]any{
					"type": "string",
				},
			},
		},
		Required: []string{"only_changed"},
	}
}

// Run executes the check_spec_conformance tool call and returns package conformance results as JSON.
//
// The call input must be a JSON-encoded checkSpecConformanceParams value. Run returns "{}" when no packages are eligible. Failures before package checking starts
// are returned as error tool results; once package checking starts, package-scoped failures are reported in the JSON result.
func (t *toolCheckSpecConformance) Run(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
	var params checkSpecConformanceParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return coretools.NewToolErrorResult(call, fmt.Sprintf("error parsing parameters: %s", err), err)
	}

	mod, err := gocode.NewModule(t.sandboxAbsDir)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}
	if t.authorizer != nil {
		if authErr := t.authorizer.IsAuthorizedForRead(false, "", ToolNameCheckSpecConformance, mod.AbsolutePath); authErr != nil {
			return coretools.NewToolErrorResult(call, authErr.Error(), authErr)
		}
	}

	explicitSelection := len(params.Packages) > 0
	var pkgs []*gocode.Package
	if explicitSelection {
		pkgs, err = t.resolveRequestedPackages(mod, params.Packages)
		if err != nil {
			return coretools.NewToolErrorResult(call, err.Error(), err)
		}
	}

	base, err := t.determineComparisonBase(ctx, mod.AbsolutePath)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	if !explicitSelection {
		pkgs, err = loadCurrentModulePackages(ctx, mod)
		if err != nil {
			return coretools.NewToolErrorResult(call, err.Error(), err)
		}
	}

	changes, err := t.collectRepoChanges(ctx, mod.AbsolutePath, base.Commit)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	eligible, err := t.findEligiblePackages(pkgs, changes, packageEligibilityOptions{
		onlyChanged:       params.OnlyChanged,
		explicitSelection: explicitSelection,
	})
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}
	if len(eligible) == 0 {
		return llmstream.ToolResult{
			CallID: call.CallID,
			Name:   call.Name,
			Type:   call.Type,
			Result: "{}",
		}
	}

	results := t.checkEligiblePackages(ctx, mod.AbsolutePath, eligible, base, changes)
	resultJSON, err := marshalPackageResults(results)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	return llmstream.ToolResult{
		CallID: call.CallID,
		Name:   call.Name,
		Type:   call.Type,
		Result: resultJSON,
	}
}

// The packageEligibilityOptions type controls how candidate packages are filtered before checks run.
type packageEligibilityOptions struct {
	onlyChanged       bool // OnlyChanged keeps only packages whose package scope changed.
	explicitSelection bool // ExplicitSelection disables CAS reuse and makes missing SPEC.md an error.
}

// The findEligiblePackages method filters pkgs to the packages that should run SPEC.md conformance checks. It sorts pkgs in place by result key, skips packages
// without SPEC.md unless explicitly selected, skips cached conforming packages unless explicitly selected, and applies the only_changed filter. Each returned entry
// contains the result key and whether the package scope changed relative to changes.
func (t *toolCheckSpecConformance) findEligiblePackages(pkgs []*gocode.Package, changes repoChanges, options packageEligibilityOptions) ([]eligiblePackage, error) {
	sort.Slice(pkgs, func(i, j int) bool {
		return packageResultKey(pkgs[i].RelativeDir) < packageResultKey(pkgs[j].RelativeDir)
	})

	eligible := make([]eligiblePackage, 0, len(pkgs))
	for _, pkg := range pkgs {
		if !packageHasSpec(pkg) {
			if options.explicitSelection {
				return nil, fmt.Errorf("explicit package %q has no SPEC.md", packageResultKey(pkg.RelativeDir))
			}
			continue
		}

		if !options.explicitSelection {
			found, conforms, err := retrieveConformanceState(pkg)
			if err != nil {
				return nil, fmt.Errorf("retrieve CAS conformance for %s: %w", packageResultKey(pkg.RelativeDir), err)
			}
			if found && conforms {
				continue
			}
		}

		hasDiff, err := packageHasChanges(pkg, changes)
		if err != nil {
			return nil, fmt.Errorf("check package diff scope for %s: %w", packageResultKey(pkg.RelativeDir), err)
		}

		if options.onlyChanged && !hasDiff {
			continue
		}

		eligible = append(eligible, eligiblePackage{
			Key:     packageResultKey(pkg.RelativeDir),
			Package: pkg,
			HasDiff: hasDiff,
		})
	}

	return eligible, nil
}

// The resolveRequestedPackages method resolves requested package names and removes duplicate package result keys. Entries may be module-relative package paths or
// current-module import paths, and the first occurrence of each resolved package is preserved.
func (t *toolCheckSpecConformance) resolveRequestedPackages(mod *gocode.Module, requested []string) ([]*gocode.Package, error) {
	seen := make(map[string]struct{}, len(requested))
	pkgs := make([]*gocode.Package, 0, len(requested))
	for _, raw := range requested {
		pkg, err := t.resolveRequestedPackage(mod, raw)
		if err != nil {
			return nil, err
		}

		key := packageResultKey(pkg.RelativeDir)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		pkgs = append(pkgs, pkg)
	}
	return pkgs, nil
}

// The resolveRequestedPackage method resolves raw as a module-relative package path or a current-module import path. It trims surrounding whitespace, rejects empty
// or absolute paths, prefers relative-path resolution, and returns a loaded package that has a SPEC.md file.
func (t *toolCheckSpecConformance) resolveRequestedPackage(mod *gocode.Module, raw string) (*gocode.Package, error) {
	requested, err := normalizeRequestedPackage(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid package %q: %w", raw, err)
	}

	if pkg, err := t.resolveRequestedPackageRelative(mod, requested); err == nil {
		return pkg, nil
	} else if !errors.Is(err, gocode.ErrResolveNotFound) {
		return nil, fmt.Errorf("invalid package %q: %w", raw, err)
	}

	pkg, err := t.resolveRequestedPackageImport(mod, requested)
	if err != nil {
		return nil, fmt.Errorf("invalid package %q: %w", raw, err)
	}
	return pkg, nil
}

// The resolveRequestedPackageRelative method resolves requested as a package directory in the current module.
//
// The returned package is loaded and guaranteed to have a SPEC.md file.
func (t *toolCheckSpecConformance) resolveRequestedPackageRelative(mod *gocode.Module, requested string) (*gocode.Package, error) {
	moduleAbsDir, _, packageRelDir, _, err := mod.ResolvePackageByRelativeDir(requested)
	if err != nil {
		return nil, err
	}
	if moduleAbsDir != mod.AbsolutePath {
		return nil, fmt.Errorf("package is not in the current module")
	}

	pkg, err := mod.LoadPackageByRelativeDir(packageRelDir)
	if err != nil {
		return nil, err
	}
	if !packageHasSpec(pkg) {
		return nil, fmt.Errorf("package has no SPEC.md")
	}
	return pkg, nil
}

// The resolveRequestedPackageImport method resolves requested as an import path in the current module.
//
// The returned package is loaded and guaranteed to have a SPEC.md file.
func (t *toolCheckSpecConformance) resolveRequestedPackageImport(mod *gocode.Module, requested string) (*gocode.Package, error) {
	moduleAbsDir, _, _, fqImportPath, err := mod.ResolvePackageByImport(requested)
	if err != nil {
		if errors.Is(err, gocode.ErrResolveNotFound) {
			return nil, err
		}
		return nil, err
	}
	if moduleAbsDir != mod.AbsolutePath {
		return nil, fmt.Errorf("package is not in the current module")
	}

	pkg, err := mod.LoadPackageByImportPath(fqImportPath)
	if err != nil {
		return nil, err
	}
	if !packageHasSpec(pkg) {
		return nil, fmt.Errorf("package has no SPEC.md")
	}
	return pkg, nil
}

func normalizeRequestedPackage(raw string) (string, error) {
	requested := strings.TrimSpace(raw)
	if requested == "" {
		return "", fmt.Errorf("package name is empty")
	}
	if strings.HasPrefix(requested, "/") {
		return "", fmt.Errorf("absolute paths are not allowed")
	}
	for strings.HasSuffix(requested, "/") && requested != "/" {
		requested = strings.TrimSuffix(requested, "/")
	}
	if requested == "" {
		return "", fmt.Errorf("package name is empty")
	}
	return requested, nil
}

// The checkEligiblePackages method checks all eligible packages and returns results keyed by package result key. Checks run concurrently up to maxConcurrency, or
// defaultMaxConcurrency when maxConcurrency is less than one. Package-scoped failures are recorded in their package results.
func (t *toolCheckSpecConformance) checkEligiblePackages(ctx context.Context, moduleAbsDir string, eligible []eligiblePackage, base comparisonBase, changes repoChanges) map[string]packageCheckResult {
	maxConcurrency := t.maxConcurrency
	if maxConcurrency <= 0 {
		maxConcurrency = defaultMaxConcurrency
	}

	results := make(map[string]packageCheckResult, len(eligible))
	var resultsMu sync.Mutex
	sem := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup

	for _, pkg := range eligible {
		pkg := pkg
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			result := t.checkPackage(ctx, moduleAbsDir, pkg, base, changes)

			resultsMu.Lock()
			results[pkg.Key] = result
			resultsMu.Unlock()
		}()
	}

	wg.Wait()
	return results
}

// The checkPackage method runs one package's SPEC.md conformance check and returns its machine-readable result. It builds diff context, invokes the package-check
// runner, validates the runner's final JSON, and records or clears cached conformance after a valid verdict. Failures before a valid verdict are returned in Error,
// while CAS failures after a valid verdict are appended to PostcheckError.
func (t *toolCheckSpecConformance) checkPackage(ctx context.Context, moduleAbsDir string, pkg eligiblePackage, base comparisonBase, changes repoChanges) packageCheckResult {
	// Once package checking begins, package-scoped failures stay in the per-package result
	// so partial successes and CAS writes are still surfaced to the caller.
	packageDiff, err := t.buildPackageDiff(ctx, moduleAbsDir, pkg.Package, base.Commit, changes)
	if err != nil {
		return packageErrorResult(fmt.Errorf("build package diff: %w", err))
	}

	specDiff := ""
	if t.specDiffContext != nil {
		specDiff, err = t.specDiffContext(pkg.Package)
		if err != nil {
			return packageErrorResult(fmt.Errorf("compute spec diff: %w", err))
		}
	}

	answer, err := t.runPackageCheck(ctx, packageCheckRequest{
		Key:            pkg.Key,
		Package:        pkg.Package,
		HasDiff:        pkg.HasDiff,
		PackageDiff:    packageDiff,
		SpecDiff:       specDiff,
		ComparisonBase: base,
	})
	if err != nil {
		return packageErrorResult(err)
	}

	result, err := parseFinalPackageCheckResult(answer, pkg.HasDiff)
	if err != nil {
		return packageErrorResult(err)
	}

	if result.Conforms != nil {
		if *result.Conforms {
			if err := t.storeConformanceState(pkg.Package); err != nil {
				result.PostcheckError = appendPackagePostcheckError(result.PostcheckError, fmt.Sprintf("store CAS conformance: %s", err))
			}
		} else {
			if err := t.deleteConformanceState(pkg.Package); err != nil {
				result.PostcheckError = appendPackagePostcheckError(result.PostcheckError, fmt.Sprintf("delete CAS conformance: %s", err))
			}
		}
	}

	return result
}

// runPackageCheckWithSubagent checks one package for SPEC.md conformance by running a limited package-mode subagent. It returns the package's JSON verdict, adding
// follow-up analysis for nonconforming results when available. Failures before a valid verdict is produced are returned as errors; analysis failures after a nonconforming
// verdict are encoded in the returned result as postcheck errors.
func (t *toolCheckSpecConformance) runPackageCheckWithSubagent(ctx context.Context, req packageCheckRequest) (string, error) {
	if t.agentInvoker == nil {
		return "", fmt.Errorf("check_spec_conformance agent unavailable")
	}

	if t.authorizer != nil {
		if authErr := t.authorizer.IsAuthorizedForRead(false, "", ToolNameCheckSpecConformance, req.Package.AbsolutePath()); authErr != nil {
			return "", authErr
		}
	}

	unit, err := codeunit.DefaultGoCodeUnit(req.Package.AbsolutePath())
	if err != nil {
		return "", err
	}

	var pkgAuthorizer authdomain.Authorizer
	if t.authorizer != nil {
		pkgAuthorizer = authdomain.NewCodeUnitAuthorizer(unit, t.authorizer)
	}

	subAgentCreatorFromContext := t.subAgentCreatorFromContext
	if subAgentCreatorFromContext == nil {
		subAgentCreatorFromContext = subAgentCreatorFromContextSafe
	}

	agentCreator, err := subAgentCreatorFromContext(ctx)
	if err != nil {
		return "", err
	}

	subagent, err := t.agentInvoker.Create(ctx, checkSpecConformanceAgentName, toolsetinterface.InvokeRequest{
		AgentCreator:     labeledSubAgentCreator{base: agentCreator, label: req.Key},
		CallerAuthorizer: pkgAuthorizer,
		CallerSandboxDir: t.sandboxAbsDir,
		ToolOptions: toolsetinterface.Options{
			SandboxDir:   t.sandboxAbsDir,
			GoPkgAbsDir:  req.Package.AbsolutePath(),
			Model:        t.model,
			AgentInvoker: t.agentInvoker,
		},
	})
	if err != nil {
		return "", err
	}

	runAgentTurn := t.runAgentTurn
	if runAgentTurn == nil {
		runAgentTurn = runPackageCheckAgentTurn
	}

	verdictAnswer, err := runAgentTurn(ctx, subagent, buildPackageCheckInstructions(req))
	if err != nil {
		return "", err
	}

	verdict, err := parsePackageCheckResult(verdictAnswer, req.HasDiff)
	if err != nil {
		return "", err
	}
	verdict = clearPackageCheckAnalysis(verdict)
	if verdict.Conforms != nil && *verdict.Conforms {
		return marshalPackageCheckResult(verdict)
	}

	analysisAnswer, err := runAgentTurn(ctx, subagent, buildPackageAnalysisInstructions(req, verdict))
	if err != nil {
		return marshalPackageCheckResultWithAnalysisFailure(verdict, err)
	}

	analysisResult, err := parsePackageAnalysisResult(analysisAnswer, verdict)
	if err != nil {
		return marshalPackageCheckResultWithAnalysisFailure(verdict, err)
	}
	finalResult, err := mergePackageCheckAnalysis(verdict, analysisResult)
	if err != nil {
		return marshalPackageCheckResultWithAnalysisFailure(verdict, err)
	}
	return marshalPackageCheckResult(finalResult)
}

func marshalPackageCheckResultWithAnalysisFailure(verdict packageCheckResult, failure error) (string, error) {
	result := verdict
	result.Nonconformances = append([]packageIssue(nil), verdict.Nonconformances...)
	analysis := fmt.Sprintf("Analysis unavailable: %s", failure)
	for i := range result.Nonconformances {
		result.Nonconformances[i].Analysis = analysis
	}
	result.PostcheckError = appendPackagePostcheckError(result.PostcheckError, fmt.Sprintf("analyze nonconformances: %s", failure))
	return marshalPackageCheckResult(result)
}

func runPackageCheckAgentTurn(ctx context.Context, subagent *agent.Agent, message string) (string, error) {
	if subagent == nil {
		return "", fmt.Errorf("check_spec_conformance agent unavailable")
	}
	return agent.CollectFinalAssistantText(ctx, subagent.SendUserMessage(ctx, message))
}

// The labeledSubAgentCreator type wraps a subagent creator and applies a stable label to every created subagent.
type labeledSubAgentCreator struct {
	base  agent.SubAgentCreator // Base is the creator that performs the actual subagent construction.
	label string                // Label is the value assigned to the created subagent.
}

// New delegates to the wrapped creator after applying c.label as the subagent label.
func (c labeledSubAgentCreator) New(systemPrompt string, tools []llmstream.Tool, options ...agent.NewOptions) (*agent.Agent, error) {
	if len(options) == 0 {
		return c.base.New(systemPrompt, tools, agent.NewOptions{SubagentLabel: c.label})
	}

	forwarded := append([]agent.NewOptions(nil), options...)
	forwarded[len(forwarded)-1].SubagentLabel = c.label
	return c.base.New(systemPrompt, tools, forwarded...)
}

// The buildPackageCheckInstructions function returns first-turn instructions for a package-check subagent to produce a strict JSON SPEC.md conformance verdict.
func buildPackageCheckInstructions(req packageCheckRequest) string {
	var body strings.Builder
	body.WriteString("Use the $spec-md check-conformance workflow for this package.\n")
	body.WriteString("This is read-only in intent.\n")
	body.WriteString("The outer tool already computed the equivalent of `codalotl spec diff` for this package. Treat that as satisfying the mechanical public-API-diff step unless you have a specific reason to distrust it.\n")
	body.WriteString("Return STRICT JSON only. No prose. No markdown fences.\n")
	body.WriteString("Allowed JSON shapes:\n")
	body.WriteString(`{"conforms":true}` + "\n")
	body.WriteString(`{"conforms":false,"nonconformances":[{"severity":"trivial|minor|major","latent":true,"message":"explanation"}]}` + "\n")
	body.WriteString("Rules:\n")
	body.WriteString("- `severity` must be one of `trivial`, `minor`, or `major`.\n")
	body.WriteString("- Set `latent=false` only when the current diff introduced the issue.\n")
	if req.HasDiff {
		body.WriteString("- This package does have a diff against the comparison base.\n")
	} else {
		body.WriteString("- This package has NO diff against the comparison base, so every nonconformance must use `latent=true`.\n")
	}
	body.WriteString("\n")
	body.WriteString("Package: ")
	body.WriteString(req.Key)
	body.WriteString("\n")
	body.WriteString("Comparison base: ")
	body.WriteString(req.ComparisonBase.describe())
	body.WriteString("\n\n")
	body.WriteString("Package diff against comparison base:\n")
	body.WriteString("```diff\n")
	if req.PackageDiff == "" {
		body.WriteString("(no diff)\n")
	} else {
		body.WriteString(req.PackageDiff)
		if !strings.HasSuffix(req.PackageDiff, "\n") {
			body.WriteString("\n")
		}
	}
	body.WriteString("```\n\n")
	body.WriteString("Precomputed spec diff context:\n")
	if req.SpecDiff == "" {
		body.WriteString("(no public API differences found by precomputed spec diff)\n")
	} else {
		body.WriteString("```\n")
		body.WriteString(req.SpecDiff)
		if !strings.HasSuffix(req.SpecDiff, "\n") {
			body.WriteString("\n")
		}
		body.WriteString("```\n")
	}
	return body.String()
}

// The buildPackageAnalysisInstructions function returns second-turn instructions for adding decision-oriented analysis to a nonconforming package verdict without
// changing the verdict itself.
func buildPackageAnalysisInstructions(req packageCheckRequest, verdict packageCheckResult) string {
	verdictJSON, err := marshalPackageCheckResult(verdict)
	if err != nil {
		verdictJSON = `{"conforms":false,"nonconformances":[]}`
	}

	var body strings.Builder
	body.WriteString("Analyze the nonconformances from your previous verdict.\n")
	body.WriteString("Do not add, remove, reorder, reword, recategorize, or change latent status for any nonconformance. The first-turn verdict is the source of truth.\n")
	body.WriteString("For each reported nonconformance, add only an `analysis` field.\n")
	body.WriteString("Return STRICT JSON only. No prose. No markdown fences.\n")
	body.WriteString("Required JSON shape:\n")
	body.WriteString(`{"conforms":false,"nonconformances":[{"severity":"trivial|minor|major","latent":true,"message":"same explanation","analysis":"decision-oriented analysis"}]}` + "\n")
	body.WriteString("For each `analysis`, answer the relevant questions:\n")
	body.WriteString("- Give 1-2 paragraph issue summary, with an example if useful.\n")
	body.WriteString("- Imagine fixing code to conform. What is size, risk, blast radius, package isolation, and public API impact?\n")
	body.WriteString("- What doe the end-user experience if this issue is triggered? What is the desired UX of the overall system?\n")
	body.WriteString("- Does changing code bring end-user value? What is it?\n")
	body.WriteString("- How likely is the user to experience this issue?\n")
	body.WriteString("- Are there any tradeoffs involved in fixing? Any UX downsides?\n")
	body.WriteString("- If there was no SPEC.md, would a human senior engineer be concerned with this? Is changing code just nitpicky without bringing actual value?\n")
	body.WriteString("- Sometimes SPEC.md is sloppily written, out of date, or simply too specific. Could that be the case here?\n")
	body.WriteString("- Overall, what is your recommendation? Change code, or change SPEC.md, or compromise?\n")
	body.WriteString("\n")
	body.WriteString("Package: ")
	body.WriteString(req.Key)
	body.WriteString("\n")
	body.WriteString("First-turn verdict JSON (preserve all fields exactly except add `analysis`):\n")
	body.WriteString("```json\n")
	body.WriteString(verdictJSON)
	body.WriteString("\n```\n")
	return body.String()
}

// The describe method returns human-readable comparison-base text, shortening the commit hash and including branch context when it is known.
func (b comparisonBase) describe() string {
	if b.ParentBranch != "" {
		return fmt.Sprintf("branch %s, parent branch %s, compare on-disk state against comparison-base commit %s", b.Branch, b.ParentBranch, shortCommit(b.Commit))
	}
	return shortCommit(b.Commit)
}

// computeSpecDiffContext returns formatted SPEC.md implementation-diff context for pkg. It returns an empty string when the package spec has no implementation diffs.
func computeSpecDiffContext(pkg *gocode.Package) (string, error) {
	specPath := filepath.Join(pkg.AbsolutePath(), "SPEC.md")
	spec, err := specmd.Read(specPath)
	if err != nil {
		return "", err
	}

	diffs, err := spec.ImplementationDiffs()
	if err != nil {
		return "", err
	}
	if len(diffs) == 0 {
		return "", nil
	}

	var buf bytes.Buffer
	if err := specmd.FormatDiffs(diffs, &buf); err != nil {
		return "", err
	}
	return strings.TrimRight(buf.String(), "\n"), nil
}

// The determineComparisonBase method selects the git commit used as the diff baseline for a conformance run. It requires a named current branch and a configured
// heuristic-base helper, and returns the current branch, optional parent branch, and comparison-base commit.
func (t *toolCheckSpecConformance) determineComparisonBase(ctx context.Context, repoAbsDir string) (comparisonBase, error) {
	branch, err := t.git.Output(ctx, repoAbsDir, "branch", "--show-current")
	if err != nil {
		return comparisonBase{}, fmt.Errorf("determine current git branch: %w", err)
	}
	branch = trimLineEndings(branch)
	if branch == "" {
		return comparisonBase{}, fmt.Errorf("unable to determine current git branch; detached HEAD is not supported for comparison-base selection")
	}

	if t.heuristicBase == nil {
		return comparisonBase{}, fmt.Errorf("determine comparison base: heuristic base unavailable")
	}

	commit, parentBranch, err := t.heuristicBase(repoAbsDir)
	if err != nil {
		return comparisonBase{}, fmt.Errorf("determine heuristic comparison base for %q: %w", branch, err)
	}
	if commit == "" {
		return comparisonBase{}, fmt.Errorf("determine heuristic comparison base for %q: empty commit", branch)
	}

	return comparisonBase{
		Branch:       branch,
		ParentBranch: parentBranch,
		Commit:       commit,
	}, nil
}

// The collectRepoChanges method returns repository paths changed since baseCommit, split into tracked and untracked paths. It includes uncommitted changes, requires
// a configured changed-paths helper, and reports paths relative to repoAbsDir.
func (t *toolCheckSpecConformance) collectRepoChanges(ctx context.Context, repoAbsDir string, baseCommit string) (repoChanges, error) {
	if t.changedPaths == nil {
		return repoChanges{}, fmt.Errorf("collect repo changes: changed-paths helper unavailable")
	}

	changedPaths, err := t.changedPaths(repoAbsDir, baseCommit, true)
	if err != nil {
		return repoChanges{}, fmt.Errorf("collect changed paths since %s: %w", shortCommit(baseCommit), err)
	}

	untrackedOut, err := t.git.Output(ctx, repoAbsDir, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return repoChanges{}, fmt.Errorf("collect untracked git changes: %w", err)
	}

	untrackedSet := make(map[string]struct{})
	for _, path := range splitNonEmptyLines(untrackedOut) {
		untrackedSet[path] = struct{}{}
	}

	tracked := make([]string, 0, len(changedPaths))
	untracked := make([]string, 0, len(changedPaths))
	for _, path := range changedPaths {
		if _, ok := untrackedSet[path]; ok {
			untracked = append(untracked, path)
			continue
		}
		tracked = append(tracked, path)
	}

	return repoChanges{
		tracked:   tracked,
		untracked: untracked,
	}, nil
}

func packageHasChanges(pkg *gocode.Package, changes repoChanges) (bool, error) {
	trackedPaths, untrackedPaths, err := packageChangedPaths(pkg, changes)
	if err != nil {
		return false, err
	}
	return len(trackedPaths) > 0 || len(untrackedPaths) > 0, nil
}

// The buildPackageDiff method returns the diff context for pkg relative to baseCommit.
//
// The diff includes tracked changes from git and synthesized diffs for untracked files in the package scope.
func (t *toolCheckSpecConformance) buildPackageDiff(ctx context.Context, repoAbsDir string, pkg *gocode.Package, baseCommit string, changes repoChanges) (string, error) {
	trackedPaths, untrackedPaths, err := packageChangedPaths(pkg, changes)
	if err != nil {
		return "", err
	}

	trackedDiff := ""
	if len(trackedPaths) > 0 {
		args := []string{"diff", "--no-ext-diff", "--relative", baseCommit, "--"}
		args = append(args, trackedPaths...)
		trackedDiff, err = t.git.Output(ctx, repoAbsDir, args...)
		if err != nil {
			return "", err
		}
	}

	var buf strings.Builder
	if trackedDiff != "" {
		buf.WriteString(trimTrailingNewline(trackedDiff))
	}

	for _, relPath := range untrackedPaths {
		untrackedDiff, err := renderUntrackedFileDiff(repoAbsDir, relPath)
		if err != nil {
			return "", err
		}
		if buf.Len() > 0 {
			buf.WriteString("\n\n")
		}
		buf.WriteString(untrackedDiff)
	}

	return buf.String(), nil
}

func renderUntrackedFileDiff(repoAbsDir string, relPath string) (string, error) {
	absPath := filepath.Join(repoAbsDir, filepath.FromSlash(relPath))
	contents, err := os.ReadFile(absPath)
	if err != nil {
		return "", err
	}

	if bytes.IndexByte(contents, 0) >= 0 {
		return fmt.Sprintf("Binary file added: %s (%d bytes)", relPath, len(contents)), nil
	}

	rendered := textdiff.DiffText("", string(contents)).RenderUnifiedDiff(false, "/dev/null", relPath, 3)
	return trimTrailingNewline(rendered), nil
}

func loadCurrentModulePackages(ctx context.Context, mod *gocode.Module) ([]*gocode.Package, error) {
	importPaths, err := currentModulePackageImportPaths(ctx, mod.AbsolutePath)
	if err != nil {
		return nil, err
	}

	pkgs := make([]*gocode.Package, 0, len(importPaths))
	for _, importPath := range importPaths {
		pkg, err := mod.LoadPackageByImportPath(importPath)
		if err != nil {
			return nil, fmt.Errorf("load package %s: %w", importPath, err)
		}
		pkgs = append(pkgs, pkg)
	}
	return pkgs, nil
}

func currentModulePackageImportPaths(ctx context.Context, moduleAbsDir string) ([]string, error) {
	goPath, err := exec.LookPath("go")
	if err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, goPath, "list", "./...")
	cmd.Dir = moduleAbsDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := trimTrailingNewline(string(out))
		if msg == "" {
			return nil, fmt.Errorf("go list ./...: %w", err)
		}
		return nil, fmt.Errorf("go list ./...: %s", msg)
	}
	return splitNonEmptyLines(string(out)), nil
}

func packageChangedPaths(pkg *gocode.Package, changes repoChanges) ([]string, []string, error) {
	scope, err := newPackageScope(pkg, changes)
	if err != nil {
		return nil, nil, err
	}

	return filterPackagePaths(scope, changes.tracked), filterPackagePaths(scope, changes.untracked), nil
}

// newPackageScope builds the path scope used to attribute repository changes to pkg. The scope uses the default Go code unit and excludes descendant package and
// hidden-directory subtrees discovered on disk or from tracked changes.
func newPackageScope(pkg *gocode.Package, changes repoChanges) (*packagePathScope, error) {
	codeUnitScope, err := codeunit.DefaultGoCodeUnit(pkg.AbsolutePath())
	if err != nil {
		return nil, err
	}

	blockedSubtrees, err := descendantPackageDirsOnDisk(pkg)
	if err != nil {
		return nil, err
	}
	blockedSubtrees = append(blockedSubtrees, descendantPackageDirsFromTrackedGoChanges(pkg, changes.tracked)...)
	hiddenSubtrees, err := descendantHiddenDirsOnDisk(pkg)
	if err != nil {
		return nil, err
	}
	blockedSubtrees = append(blockedSubtrees, hiddenSubtrees...)
	blockedSubtrees = append(blockedSubtrees, descendantHiddenDirsFromTrackedChanges(pkg, changes.tracked)...)

	return &packagePathScope{
		codeUnit:        codeUnitScope,
		moduleAbsDir:    pkg.Module.AbsolutePath,
		packageRelDir:   normalizeRelativeDir(pkg.RelativeDir),
		blockedSubtrees: compactRelativeDirs(blockedSubtrees),
	}, nil
}

// descendantPackageDirsOnDisk returns descendant directories under pkg that contain Go files. Returned directories are module-relative slash paths. It excludes
// pkg itself, skips testdata descendants, and does not descend into reported package directories.
func descendantPackageDirsOnDisk(pkg *gocode.Package) ([]string, error) {
	rootAbsDir := pkg.AbsolutePath()
	moduleAbsDir := pkg.Module.AbsolutePath
	var descendantDirs []string

	err := filepath.WalkDir(rootAbsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() || path == rootAbsDir {
			return nil
		}

		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
				continue
			}
			relDir, err := filepath.Rel(moduleAbsDir, path)
			if err != nil {
				return err
			}
			if relativePathContainsDir(relDir, "testdata") {
				return filepath.SkipDir
			}
			descendantDirs = append(descendantDirs, filepath.ToSlash(relDir))
			return filepath.SkipDir
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return descendantDirs, nil
}

// descendantHiddenDirsOnDisk returns hidden descendant directories that currently exist under pkg. Returned directories are module-relative slash paths. The package
// root itself is not included.
func descendantHiddenDirsOnDisk(pkg *gocode.Package) ([]string, error) {
	rootAbsDir := pkg.AbsolutePath()
	moduleAbsDir := pkg.Module.AbsolutePath
	var hiddenDirs []string

	err := filepath.WalkDir(rootAbsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() || path == rootAbsDir || !strings.HasPrefix(d.Name(), ".") {
			return nil
		}

		relDir, err := filepath.Rel(moduleAbsDir, path)
		if err != nil {
			return err
		}
		hiddenDirs = append(hiddenDirs, filepath.ToSlash(relDir))
		return filepath.SkipDir
	})
	if err != nil {
		return nil, err
	}

	return hiddenDirs, nil
}

func descendantPackageDirsFromTrackedGoChanges(pkg *gocode.Package, trackedPaths []string) []string {
	packageRelDir := normalizeRelativeDir(pkg.RelativeDir)
	descendantDirs := make([]string, 0, len(trackedPaths))
	for _, relPath := range trackedPaths {
		relPath = filepath.ToSlash(relPath)
		if !strings.HasSuffix(relPath, ".go") || !pathWithinRelativeDir(relPath, packageRelDir) || relativePathContainsDir(relPath, "testdata") {
			continue
		}

		dir := normalizeRelativeDir(filepath.Dir(relPath))
		if dir == packageRelDir {
			continue
		}
		descendantDirs = append(descendantDirs, dir)
	}
	return descendantDirs
}

func descendantHiddenDirsFromTrackedChanges(pkg *gocode.Package, trackedPaths []string) []string {
	packageRelDir := normalizeRelativeDir(pkg.RelativeDir)
	hiddenDirs := make([]string, 0, len(trackedPaths))
	for _, relPath := range trackedPaths {
		relPath = filepath.ToSlash(relPath)
		if !pathWithinRelativeDir(relPath, packageRelDir) {
			continue
		}

		hiddenDirs = append(hiddenDirs, hiddenAncestorDirs(relPath, packageRelDir)...)
	}
	return hiddenDirs
}

func filterPackagePaths(scope *packagePathScope, relPaths []string) []string {
	filtered := make([]string, 0, len(relPaths))
	for _, relPath := range relPaths {
		if pathInPackage(scope, relPath) {
			filtered = append(filtered, relPath)
		}
	}
	return filtered
}

// The pathInPackage function reports whether a repository-relative path belongs to scope. Existing paths are checked with the code unit; missing paths are checked
// against the package directory and excluded subtrees.
func pathInPackage(scope *packagePathScope, relPath string) bool {
	absPath := filepath.Join(scope.moduleAbsDir, filepath.FromSlash(relPath))
	if scope.codeUnit.Includes(absPath) {
		return true
	}
	if pathExists(absPath) {
		return false
	}

	relPath = filepath.ToSlash(relPath)
	if !pathWithinRelativeDir(relPath, scope.packageRelDir) {
		return false
	}
	for _, blockedDir := range scope.blockedSubtrees {
		if pathWithinRelativeDir(relPath, blockedDir) {
			return false
		}
	}
	return true
}

func normalizeRelativeDir(relDir string) string {
	relDir = filepath.ToSlash(relDir)
	if relDir == "." {
		return ""
	}
	return relDir
}

func pathWithinRelativeDir(relPath string, relDir string) bool {
	relPath = filepath.ToSlash(relPath)
	relDir = normalizeRelativeDir(relDir)
	if relDir == "" {
		return relPath != ""
	}
	return relPath == relDir || strings.HasPrefix(relPath, relDir+"/")
}

// compactRelativeDirs returns a sorted minimal set of non-root relative directories. It removes duplicates and drops directories already covered by an included
// ancestor.
func compactRelativeDirs(relDirs []string) []string {
	if len(relDirs) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(relDirs))
	unique := make([]string, 0, len(relDirs))
	for _, relDir := range relDirs {
		relDir = normalizeRelativeDir(relDir)
		if relDir == "" {
			continue
		}
		if _, ok := seen[relDir]; ok {
			continue
		}
		seen[relDir] = struct{}{}
		unique = append(unique, relDir)
	}
	sort.Strings(unique)

	compacted := make([]string, 0, len(unique))
	for _, relDir := range unique {
		covered := false
		for _, kept := range compacted {
			if pathWithinRelativeDir(relDir, kept) {
				covered = true
				break
			}
		}
		if !covered {
			compacted = append(compacted, relDir)
		}
	}

	return compacted
}

// hiddenAncestorDirs returns hidden ancestor directories of relPath below packageRelDir. Returned directories are module-relative slash paths. It returns nil when
// relPath is outside packageRelDir.
func hiddenAncestorDirs(relPath string, packageRelDir string) []string {
	relPath = filepath.ToSlash(relPath)
	packageRelDir = normalizeRelativeDir(packageRelDir)
	if !pathWithinRelativeDir(relPath, packageRelDir) {
		return nil
	}

	parts := strings.Split(relPath, "/")
	start := 0
	if packageRelDir != "" {
		start = len(strings.Split(packageRelDir, "/"))
	}

	hiddenDirs := make([]string, 0, len(parts)-start)
	for i := start; i < len(parts)-1; i++ {
		if !strings.HasPrefix(parts[i], ".") {
			continue
		}
		hiddenDirs = append(hiddenDirs, strings.Join(parts[:i+1], "/"))
	}
	return hiddenDirs
}

func relativePathContainsDir(relPath string, dirName string) bool {
	for _, part := range strings.Split(filepath.ToSlash(relPath), "/") {
		if part == dirName {
			return true
		}
	}
	return false
}

func packageResultKey(relativeDir string) string {
	if relativeDir == "" || relativeDir == "." {
		return "."
	}
	return filepath.ToSlash(relativeDir)
}

// ParseCheckSpecConformanceResults parses the raw machine-readable JSON tool result.
func ParseCheckSpecConformanceResults(raw string) (CheckSpecConformanceResults, error) {
	var results CheckSpecConformanceResults
	if err := json.Unmarshal([]byte(raw), &results); err != nil {
		return nil, err
	}

	for key, result := range results {
		validated, err := validatePackageCheckResult(packageCheckResult(result), packageResultValidationOptions{
			allowError:          true,
			allowPostcheckError: true,
			requireAnalysis:     true,
		})
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		results[key] = validated
	}

	return results, nil
}

// SummarizeCheckSpecConformanceResults computes sorted package buckets and counts.
func SummarizeCheckSpecConformanceResults(results CheckSpecConformanceResults) CheckSpecConformanceSummary {
	keys := make([]string, 0, len(results))
	for key := range results {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	summary := CheckSpecConformanceSummary{
		ConformingPackages:    make([]string, 0, len(results)),
		NonconformingPackages: make([]string, 0, len(results)),
		ErrorPackages:         make([]string, 0, len(results)),
		PostcheckErrors:       make([]CheckSpecConformancePostcheckError, 0),
	}
	for _, key := range keys {
		result := packageCheckResult(results[key])
		switch {
		case result.Error != "":
			summary.ErrorPackages = append(summary.ErrorPackages, key)
		case result.Conforms != nil && *result.Conforms:
			summary.ConformingPackages = append(summary.ConformingPackages, key)
		default:
			summary.NonconformingPackages = append(summary.NonconformingPackages, key)
		}

		if result.PostcheckError != "" {
			summary.PostcheckErrors = append(summary.PostcheckErrors, CheckSpecConformancePostcheckError{
				Package: key,
				Error:   result.PostcheckError,
			})
		}
	}

	summary.ConformingCount = len(summary.ConformingPackages)
	summary.NonconformingCount = len(summary.NonconformingPackages)
	summary.ErrorCount = len(summary.ErrorPackages)
	return summary
}

func formatCheckSpecConformanceCompactCompletion(raw string) llmstream.Block {
	results, err := ParseCheckSpecConformanceResults(raw)
	if err != nil {
		return invalidCheckSpecConformanceResultBlock()
	}

	summary := SummarizeCheckSpecConformanceResults(results)
	return formatCompactCheckSpecConformanceSummaryBlock(summary)
}

func formatCheckSpecConformancePackageFinalMessage(finalMessage string) llmstream.Block {
	result, err := parsePackageFinalMessageResult(finalMessage)
	if err != nil {
		return invalidConformanceResultBlock()
	}

	return formatPackageCheckResultBlock(result)
}

func parsePackageCheckResult(answer string, hasDiff bool) (packageCheckResult, error) {
	payload := extractJSONObject(answer)
	if payload == "" {
		return packageCheckResult{}, fmt.Errorf("subagent returned non-JSON result")
	}

	result, err := decodePackageCheckResult(payload)
	if err != nil {
		return packageCheckResult{}, err
	}

	return validatePackageCheckResult(result, packageResultValidationOptions{
		hasDiff:             &hasDiff,
		allowError:          false,
		allowPostcheckError: false,
	})
}

func parseFinalPackageCheckResult(answer string, hasDiff bool) (packageCheckResult, error) {
	payload := extractJSONObject(answer)
	if payload == "" {
		return packageCheckResult{}, fmt.Errorf("subagent returned non-JSON result")
	}

	result, err := decodePackageCheckResult(payload)
	if err != nil {
		return packageCheckResult{}, err
	}

	return validatePackageCheckResult(result, packageResultValidationOptions{
		hasDiff:             &hasDiff,
		allowError:          false,
		allowPostcheckError: true,
		requireAnalysis:     true,
	})
}

// parsePackageAnalysisResult extracts a package-check JSON object from answer and validates it as follow-up analysis for verdict. The result must keep the package
// nonconforming, preserve the original issues, and add analysis text to each issue.
func parsePackageAnalysisResult(answer string, verdict packageCheckResult) (packageCheckResult, error) {
	payload := extractJSONObject(answer)
	if payload == "" {
		return packageCheckResult{}, fmt.Errorf("subagent returned non-JSON analysis result")
	}

	result, err := decodePackageCheckResult(payload)
	if err != nil {
		return packageCheckResult{}, err
	}

	analysisResult, err := validatePackageCheckResult(result, packageResultValidationOptions{
		allowError:          false,
		allowPostcheckError: false,
		requireAnalysis:     true,
	})
	if err != nil {
		return packageCheckResult{}, err
	}
	if analysisResult.Conforms == nil || *analysisResult.Conforms {
		return packageCheckResult{}, fmt.Errorf("analysis result must preserve conforms=false")
	}
	if err := validateAnalysisMatchesVerdict(verdict, analysisResult); err != nil {
		return packageCheckResult{}, err
	}
	return analysisResult, nil
}

// The extractJSONObject function extracts an object-shaped JSON payload from raw, fenced, or text-wrapped model output. It returns an empty string when no such
// payload is found; callers are responsible for JSON decoding.
func extractJSONObject(answer string) string {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return ""
	}

	if strings.HasPrefix(answer, "```") {
		lines := strings.Split(answer, "\n")
		if len(lines) >= 2 && strings.HasPrefix(lines[0], "```") {
			last := len(lines) - 1
			for last >= 1 && strings.TrimSpace(lines[last]) == "" {
				last--
			}
			if last >= 1 && strings.TrimSpace(lines[last]) == "```" {
				answer = strings.Join(lines[1:last], "\n")
			}
		}
	}

	answer = strings.TrimSpace(answer)
	if strings.HasPrefix(answer, "{") && strings.HasSuffix(answer, "}") {
		return answer
	}

	start := strings.IndexByte(answer, '{')
	end := strings.LastIndexByte(answer, '}')
	if start >= 0 && end > start {
		return answer[start : end+1]
	}
	return ""
}

func parsePackageFinalMessageResult(finalMessage string) (packageCheckResult, error) {
	payload := extractJSONObject(finalMessage)
	if payload == "" {
		return packageCheckResult{}, fmt.Errorf("subagent returned non-JSON result")
	}

	result, err := decodePackageCheckResult(payload)
	if err != nil {
		return packageCheckResult{}, err
	}

	return validatePackageCheckResult(result, packageResultValidationOptions{
		allowError:          false,
		allowPostcheckError: false,
	})
}

func decodePackageCheckResult(payload string) (packageCheckResult, error) {
	var result packageCheckResult
	decoder := json.NewDecoder(strings.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return packageCheckResult{}, fmt.Errorf("decode subagent JSON: %w", err)
	}
	return result, nil
}

// The validatePackageCheckResult function verifies that result has a permitted package-result shape and returns the normalized result. When options.hasDiff is set
// to false, it marks all reported nonconformances as latent.
func validatePackageCheckResult(result packageCheckResult, options packageResultValidationOptions) (packageCheckResult, error) {
	if result.Error != "" {
		if !options.allowError {
			return packageCheckResult{}, fmt.Errorf("subagent returned unexpected error payload: %s", result.Error)
		}
		if result.Conforms != nil || result.Nonconformances != nil || result.PostcheckError != "" {
			return packageCheckResult{}, fmt.Errorf("error result cannot include verdict fields")
		}
		return result, nil
	}

	if result.PostcheckError != "" && !options.allowPostcheckError {
		return packageCheckResult{}, fmt.Errorf("subagent returned unexpected postcheck_error payload: %s", result.PostcheckError)
	}
	if result.Conforms == nil {
		return packageCheckResult{}, fmt.Errorf("subagent JSON must include conforms")
	}

	if *result.Conforms {
		if result.Nonconformances != nil {
			return packageCheckResult{}, fmt.Errorf("subagent returned nonconformances for conforms=true")
		}
		return result, nil
	}
	if len(result.Nonconformances) == 0 {
		return packageCheckResult{}, fmt.Errorf("subagent returned conforms=false without nonconformances")
	}

	for i := range result.Nonconformances {
		switch result.Nonconformances[i].Severity {
		case "trivial", "minor", "major":
		default:
			return packageCheckResult{}, fmt.Errorf("subagent returned invalid severity %q", result.Nonconformances[i].Severity)
		}
		if result.Nonconformances[i].Message == "" {
			return packageCheckResult{}, fmt.Errorf("subagent returned a nonconformance with an empty message")
		}
		if options.requireAnalysis && result.Nonconformances[i].Analysis == "" {
			return packageCheckResult{}, fmt.Errorf("subagent returned a nonconformance without analysis")
		}
		if options.hasDiff != nil && !*options.hasDiff {
			result.Nonconformances[i].Latent = true
		}
	}

	return result, nil
}

// The validateAnalysisMatchesVerdict function reports whether analysisResult preserves verdict and only adds analysis text.
func validateAnalysisMatchesVerdict(verdict packageCheckResult, analysisResult packageCheckResult) error {
	if verdict.Conforms == nil || *verdict.Conforms {
		return fmt.Errorf("analysis requested for conforming verdict")
	}
	if len(verdict.Nonconformances) != len(analysisResult.Nonconformances) {
		return fmt.Errorf("analysis result changed nonconformance count")
	}
	for i := range verdict.Nonconformances {
		original := verdict.Nonconformances[i]
		analyzed := analysisResult.Nonconformances[i]
		if analyzed.Severity != original.Severity {
			return fmt.Errorf("analysis result changed severity for nonconformance %d", i+1)
		}
		if analyzed.Latent != original.Latent {
			return fmt.Errorf("analysis result changed latent status for nonconformance %d", i+1)
		}
		if analyzed.Message != original.Message {
			return fmt.Errorf("analysis result changed message for nonconformance %d", i+1)
		}
	}
	return nil
}

func mergePackageCheckAnalysis(verdict packageCheckResult, analysisResult packageCheckResult) (packageCheckResult, error) {
	if err := validateAnalysisMatchesVerdict(verdict, analysisResult); err != nil {
		return packageCheckResult{}, err
	}

	merged := verdict
	merged.Nonconformances = append([]packageIssue(nil), verdict.Nonconformances...)
	for i := range merged.Nonconformances {
		merged.Nonconformances[i].Analysis = analysisResult.Nonconformances[i].Analysis
	}
	return merged, nil
}

func clearPackageCheckAnalysis(result packageCheckResult) packageCheckResult {
	if len(result.Nonconformances) == 0 {
		return result
	}

	result.Nonconformances = append([]packageIssue(nil), result.Nonconformances...)
	for i := range result.Nonconformances {
		result.Nonconformances[i].Analysis = ""
	}
	return result
}

// formatPackageCheckResultBlock formats a package-check result for package-slot display. It renders errors, conforming verdicts, or nonconforming issues in human-readable
// form and omits analysis text.
func formatPackageCheckResultBlock(result packageCheckResult) llmstream.Block {
	if result.Error != "" {
		return llmstream.Paragraph{
			Lines: []llmstream.Line{{
				Segments: []llmstream.Segment{
					{Text: "Error: " + result.Error, Role: llmstream.RoleError},
				},
			}},
		}
	}
	if result.Conforms != nil && *result.Conforms {
		return llmstream.Paragraph{
			Lines: []llmstream.Line{{
				Segments: []llmstream.Segment{
					{Text: "Conforms", Role: llmstream.RoleSuccess},
				},
			}},
		}
	}

	lines := []llmstream.Line{{
		Segments: []llmstream.Segment{
			{Text: "Non-conforming", Role: llmstream.RoleAccent},
		},
	}}
	for _, issue := range result.Nonconformances {
		scope := "New"
		if issue.Latent {
			scope = "Latent"
		}
		lines = append(lines, llmstream.Line{
			Segments: []llmstream.Segment{
				{Text: fmt.Sprintf("[%s][%s] %s", scope, issue.Severity, issue.Message), Role: llmstream.RoleNormal},
			},
		})
	}
	return llmstream.Paragraph{Lines: lines}
}

func packageErrorResult(err error) packageCheckResult {
	return packageCheckResult{Error: err.Error()}
}

func appendPackagePostcheckError(existing string, next string) string {
	if existing == "" {
		return next
	}
	if next == "" {
		return existing
	}
	return existing + "; " + next
}

func marshalPackageCheckResult(result packageCheckResult) (string, error) {
	b, err := json.MarshalIndent(result, "", "    ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func marshalPackageResults(results map[string]packageCheckResult) (string, error) {
	b, err := json.MarshalIndent(results, "", "    ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func retrieveConformanceState(pkg *gocode.Package) (bool, bool, error) {
	db, casRoot, err := conformanceCASDB(pkg)
	if err != nil {
		return false, false, err
	}
	info, err := os.Stat(casRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return false, false, nil
		}
		return false, false, err
	}
	if !info.IsDir() {
		return false, false, fmt.Errorf("%q is not a directory", casRoot)
	}
	return casconformance.Retrieve(db, pkg)
}

// The storeConformanceState method records that pkg conforms to its SPEC.md in CAS.
func (t *toolCheckSpecConformance) storeConformanceState(pkg *gocode.Package) error {
	db, casRoot, err := conformanceCASDB(pkg)
	if err != nil {
		return err
	}
	if t.authorizer != nil {
		if authErr := t.authorizer.IsAuthorizedForWrite(false, "", ToolNameCheckSpecConformance, casRoot); authErr != nil {
			return authErr
		}
	}
	if err := os.MkdirAll(casRoot, 0o755); err != nil {
		return err
	}
	return casconformance.Store(db, pkg, true)
}

// The deleteConformanceState method removes cached SPEC conformance metadata for pkg.
//
// Deleting from a missing CAS root is a no-op.
func (t *toolCheckSpecConformance) deleteConformanceState(pkg *gocode.Package) error {
	db, casRoot, err := conformanceCASDB(pkg)
	if err != nil {
		return err
	}
	if !pathExists(casRoot) {
		return nil
	}
	if t.authorizer != nil {
		if authErr := t.authorizer.IsAuthorizedForWrite(false, "", ToolNameCheckSpecConformance, casRoot); authErr != nil {
			return authErr
		}
	}
	return casconformance.Delete(db, pkg)
}

func conformanceCASDB(pkg *gocode.Package) (*gocas.DB, string, error) {
	db, err := gocas.NewDBForBaseDir(pkg.Module.AbsolutePath)
	if err != nil {
		return nil, "", err
	}
	return db, db.DB.AbsRoot, nil
}

func packageHasSpec(pkg *gocode.Package) bool {
	return pathExists(filepath.Join(pkg.AbsolutePath(), "SPEC.md"))
}

func pathExists(absPath string) bool {
	_, err := os.Stat(absPath)
	return err == nil
}

func splitNonEmptyLines(s string) []string {
	lines := strings.Split(trimTrailingNewline(s), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func trimTrailingNewline(s string) string {
	s = strings.TrimSuffix(s, "\n")
	s = strings.TrimSuffix(s, "\r")
	return s
}

func trimLineEndings(s string) string {
	return trimTrailingNewline(s)
}

func nounForCount(count int, singular string, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}

func shortCommit(commit string) string {
	if len(commit) <= 12 {
		return commit
	}
	return commit[:12]
}

func invalidConformanceResultBlock() llmstream.Block {
	return llmstream.Paragraph{
		Lines: []llmstream.Line{{
			Segments: []llmstream.Segment{
				{Text: "Invalid conformance result", Role: llmstream.RoleError},
			},
		}},
	}
}

func invalidCheckSpecConformanceResultBlock() llmstream.Block {
	return llmstream.Paragraph{
		Lines: []llmstream.Line{{
			Segments: []llmstream.Segment{
				{Text: "Invalid check_spec_conformance result", Role: llmstream.RoleError},
			},
		}},
	}
}

func checkSpecConformanceSummaryLine(summary CheckSpecConformanceSummary) llmstream.Line {
	return llmstream.Line{
		JoinWithSpace: true,
		Segments: []llmstream.Segment{
			{Text: fmt.Sprintf("%d conforming,", summary.ConformingCount), Role: llmstream.RoleAccent},
			{Text: fmt.Sprintf("%d non-conforming,", summary.NonconformingCount), Role: llmstream.RoleAccent},
			{Text: fmt.Sprintf("%d %s", summary.ErrorCount, nounForCount(summary.ErrorCount, "error", "errors")), Role: llmstream.RoleAccent},
		},
	}
}

func formatCompactCheckSpecConformanceSummaryBlock(summary CheckSpecConformanceSummary) llmstream.Block {
	lines := []llmstream.Line{checkSpecConformanceSummaryLine(summary)}
	for _, postcheckErr := range summary.PostcheckErrors {
		lines = append(lines, llmstream.Line{
			Segments: []llmstream.Segment{
				{Text: fmt.Sprintf("%s: %s", postcheckErr.Package, postcheckErr.Error), Role: llmstream.RoleNormal},
			},
		})
	}
	return llmstream.Paragraph{Lines: lines}
}

// Output runs git with args in repoAbsDir and returns the command's combined output. It honors ctx and returns an error with the git invocation and command output
// when the command fails.
func (execGitRunner) Output(ctx context.Context, repoAbsDir string, args ...string) (string, error) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return "", err
	}

	cmd := exec.CommandContext(ctx, gitPath, args...)
	cmd.Dir = repoAbsDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := trimTrailingNewline(string(out))
		if msg == "" {
			return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return string(out), nil
}

func subAgentCreatorFromContextSafe(ctx context.Context) (creator agent.SubAgentCreator, err error) {
	defer func() {
		if recover() != nil {
			creator = nil
			err = fmt.Errorf("unable to create subagent")
		}
	}()

	creator = agent.SubAgentCreatorFromContext(ctx)
	if creator == nil {
		return nil, fmt.Errorf("unable to create subagent")
	}
	return creator, nil
}

var checkSpecConformancePresenterInstance llmstream.Presenter = checkSpecConformancePresenter{}

// The checkSpecConformancePresenter type renders check_spec_conformance progress, summaries, and package subagent results.
type checkSpecConformancePresenter struct{}

// SubagentFinalMessage formats a package-check subagent's final JSON as human-readable status instead of raw JSON.
func (checkSpecConformancePresenter) SubagentFinalMessage(_ llmstream.ToolCall, _ string, finalMessage string) llmstream.Block {
	return formatCheckSpecConformancePackageFinalMessage(finalMessage)
}

// Present returns the progress or completion presentation for a check_spec_conformance tool call. A nil result produces the in-progress presentation; a successful
// result may include a compact summary body.
func (checkSpecConformancePresenter) Present(call llmstream.ToolCall, result *llmstream.ToolResult) llmstream.Presentation {
	action := "Checking SPEC conformance"
	if result != nil {
		action = "Checked SPEC conformance"
	}

	presentation := llmstream.Presentation{
		Behavior: llmstream.CompletionBehaviorAppend,
		Summary: llmstream.Line{
			Segments: []llmstream.Segment{
				{Text: action, Role: llmstream.RoleAction},
			},
		},
	}
	if result == nil || result.IsError {
		return presentation
	}

	body, ok := presentCheckSpecConformanceBody(result.Result)
	if ok {
		presentation.Body = body
	}
	return presentation
}

func presentCheckSpecConformanceBody(raw string) (llmstream.Block, bool) {
	results, err := ParseCheckSpecConformanceResults(raw)
	if err != nil {
		return nil, false
	}
	if len(results) == 0 {
		return llmstream.Paragraph{
			Lines: []llmstream.Line{{
				Segments: []llmstream.Segment{
					{Text: "No eligible packages.", Role: llmstream.RoleAccent},
				},
			}},
		}, true
	}

	return formatCompactCheckSpecConformanceSummaryBlock(SummarizeCheckSpecConformanceResults(results)), true
}
