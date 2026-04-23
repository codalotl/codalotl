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
	"github.com/codalotl/codalotl/internal/q/cas"
	"github.com/codalotl/codalotl/internal/specmd"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/codalotl/codalotl/internal/tools/coretools"
	"github.com/codalotl/codalotl/internal/tools/toolsetinterface"
)

//go:embed check_spec_conformance.md
var descriptionCheckSpecConformance string

const ToolNameCheckSpecConformance = "check_spec_conformance"

const defaultMaxConcurrency = 5

// This mirrors internal/agentbuilder.AgentLimitedPackageMode without importing that package and creating an import cycle.
const checkSpecConformanceAgentName = "limited_package_mode"

type CheckSpecConformanceToolOptions struct {
	AgentInvoker   toolsetinterface.AgentInvoker
	Model          llmmodel.ModelID
	MaxConcurrency int // 0: use default concurrency
}

type toolCheckSpecConformance struct {
	sandboxAbsDir  string
	authorizer     authdomain.Authorizer
	agentInvoker   toolsetinterface.AgentInvoker
	model          llmmodel.ModelID
	maxConcurrency int

	git                        gitRunner
	specDiffContext            func(pkg *gocode.Package) (string, error)
	heuristicBase              func(repoDir string) (commit string, ref string, err error)
	changedPaths               func(repoDir string, baseCommit string, includeUncommitted bool) ([]string, error)
	runPackageCheck            packageCheckRunner
	subAgentCreatorFromContext func(ctx context.Context) (agent.SubAgentCreator, error)
}

type checkSpecConformanceParams struct {
	OnlyChanged bool     `json:"only_changed"`
	Packages    []string `json:"packages"`
}

type comparisonBase struct {
	Branch       string
	ParentBranch string
	Commit       string
}

type repoChanges struct {
	tracked   []string
	untracked []string
}

type packagePathScope struct {
	codeUnit        *codeunit.CodeUnit
	moduleAbsDir    string
	packageRelDir   string
	blockedSubtrees []string
}

type eligiblePackage struct {
	Key     string
	Package *gocode.Package
	HasDiff bool
}

type packageCheckRequest struct {
	Key            string
	Package        *gocode.Package
	HasDiff        bool
	PackageDiff    string
	SpecDiff       string
	ComparisonBase comparisonBase
}

type packageCheckRunner func(ctx context.Context, req packageCheckRequest) (string, error)

type packageCheckResult struct {
	Conforms        *bool          `json:"conforms,omitempty"`
	Nonconformances []packageIssue `json:"nonconformances,omitempty"`
	Error           string         `json:"error,omitempty"`
	PostcheckError  string         `json:"postcheck_error,omitempty"`
}

type packageIssue struct {
	Severity string `json:"severity"`
	Latent   bool   `json:"latent"`
	Message  string `json:"message"`
}

// CheckSpecConformancePackageResult is one package entry in a check_spec_conformance tool result.
type CheckSpecConformancePackageResult = packageCheckResult

// CheckSpecConformanceIssue is one reported SPEC.md nonconformance.
type CheckSpecConformanceIssue = packageIssue

// CheckSpecConformanceResults is the parsed raw JSON result of check_spec_conformance.
type CheckSpecConformanceResults map[string]CheckSpecConformancePackageResult

// CheckSpecConformanceSummary groups package result counts and sorted package keys.
type CheckSpecConformanceSummary struct {
	ConformingCount       int
	NonconformingCount    int
	ErrorCount            int
	ConformingPackages    []string
	NonconformingPackages []string
	ErrorPackages         []string
	PostcheckErrors       []CheckSpecConformancePostcheckError
}

// CheckSpecConformancePostcheckError is a package-scoped failure that happened after a valid verdict.
type CheckSpecConformancePostcheckError struct {
	Package string
	Error   string
}

type packageResultValidationOptions struct {
	allowError          bool
	allowPostcheckError bool
	hasDiff             *bool
}

type gitRunner interface {
	Output(ctx context.Context, repoAbsDir string, args ...string) (string, error)
}

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
	}
	tool.runPackageCheck = tool.runPackageCheckWithSubagent
	return tool
}

func (t *toolCheckSpecConformance) Name() string {
	return ToolNameCheckSpecConformance
}

func (t *toolCheckSpecConformance) Presenter() llmstream.Presenter {
	return checkSpecConformancePresenterInstance
}

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
				"description": "Optional package list. Entries may be current-module import paths or module-relative package paths. Omit, null, or empty to use default discovery.",
				"items": map[string]any{
					"type": "string",
				},
			},
		},
		Required: []string{"only_changed"},
	}
}

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

type packageEligibilityOptions struct {
	onlyChanged       bool
	explicitSelection bool
}

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

	result, err := parsePackageCheckResult(answer, pkg.HasDiff)
	if err != nil {
		return packageErrorResult(err)
	}

	if result.Conforms != nil {
		if *result.Conforms {
			if err := t.storeConformanceState(pkg.Package); err != nil {
				result.PostcheckError = fmt.Sprintf("store CAS conformance: %s", err)
			}
		} else {
			if err := t.deleteConformanceState(pkg.Package); err != nil {
				result.PostcheckError = fmt.Sprintf("delete CAS conformance: %s", err)
			}
		}
	}

	return result
}

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

	instructions := buildPackageCheckInstructions(req)
	events, err := t.agentInvoker.Invoke(ctx, checkSpecConformanceAgentName, toolsetinterface.InvokeRequest{
		AgentCreator:     labeledSubAgentCreator{base: agentCreator, label: req.Key},
		CallerAuthorizer: pkgAuthorizer,
		CallerSandboxDir: t.sandboxAbsDir,
		ToolOptions: toolsetinterface.Options{
			SandboxDir:   t.sandboxAbsDir,
			GoPkgAbsDir:  req.Package.AbsolutePath(),
			Model:        t.model,
			AgentInvoker: t.agentInvoker,
		},
		Messages: []string{instructions},
	})
	if err != nil {
		return "", err
	}

	return agent.CollectFinalAssistantText(ctx, events)
}

type labeledSubAgentCreator struct {
	base  agent.SubAgentCreator
	label string
}

func (c labeledSubAgentCreator) New(systemPrompt string, tools []llmstream.Tool, options ...agent.NewOptions) (*agent.Agent, error) {
	if len(options) == 0 {
		return c.base.New(systemPrompt, tools, agent.NewOptions{SubagentLabel: c.label})
	}

	forwarded := append([]agent.NewOptions(nil), options...)
	forwarded[len(forwarded)-1].SubagentLabel = c.label
	return c.base.New(systemPrompt, tools, forwarded...)
}

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

func (b comparisonBase) describe() string {
	if b.ParentBranch != "" {
		return fmt.Sprintf("branch %s, parent branch %s, compare on-disk state against comparison-base commit %s", b.Branch, b.ParentBranch, shortCommit(b.Commit))
	}
	return shortCommit(b.Commit)
}

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
		if options.hasDiff != nil && !*options.hasDiff {
			result.Nonconformances[i].Latent = true
		}
	}

	return result, nil
}

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

func marshalPackageResults(results map[string]packageCheckResult) (string, error) {
	b, err := json.MarshalIndent(results, "", "    ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func retrieveConformanceState(pkg *gocode.Package) (bool, bool, error) {
	casRoot := filepath.Join(pkg.Module.AbsolutePath, ".codalotl", "cas")
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
	return casconformance.Retrieve(newCASDB(pkg.Module.AbsolutePath), pkg)
}

func (t *toolCheckSpecConformance) storeConformanceState(pkg *gocode.Package) error {
	casRoot := filepath.Join(pkg.Module.AbsolutePath, ".codalotl", "cas")
	if t.authorizer != nil {
		if authErr := t.authorizer.IsAuthorizedForWrite(false, "", ToolNameCheckSpecConformance, casRoot); authErr != nil {
			return authErr
		}
	}
	if err := os.MkdirAll(casRoot, 0o755); err != nil {
		return err
	}
	return casconformance.Store(newCASDB(pkg.Module.AbsolutePath), pkg, true)
}

func (t *toolCheckSpecConformance) deleteConformanceState(pkg *gocode.Package) error {
	casRoot := filepath.Join(pkg.Module.AbsolutePath, ".codalotl", "cas")
	if !pathExists(casRoot) {
		return nil
	}
	if t.authorizer != nil {
		if authErr := t.authorizer.IsAuthorizedForWrite(false, "", ToolNameCheckSpecConformance, casRoot); authErr != nil {
			return authErr
		}
	}
	return casconformance.Delete(newCASDB(pkg.Module.AbsolutePath), pkg)
}

func newCASDB(moduleAbsDir string) *gocas.DB {
	return &gocas.DB{
		BaseDir: moduleAbsDir,
		DB: cas.DB{
			AbsRoot: filepath.Join(moduleAbsDir, ".codalotl", "cas"),
		},
	}
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

type checkSpecConformancePresenter struct{}

func (checkSpecConformancePresenter) SubagentFinalMessage(_ llmstream.ToolCall, _ string, finalMessage string) llmstream.Block {
	return formatCheckSpecConformancePackageFinalMessage(finalMessage)
}

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
