package spectools

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
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

	git             gitRunner
	specDiffContext func(pkg *gocode.Package) (string, error)
	runPackageCheck packageCheckRunner
}

type checkSpecConformanceParams struct {
	OnlyChanged bool `json:"only_changed"`
}

type comparisonBaseMode string

const (
	comparisonBaseModeHEAD        comparisonBaseMode = "head"
	comparisonBaseModeBranchPoint comparisonBaseMode = "branch_point"
)

type comparisonBase struct {
	Branch       string
	ParentBranch string
	Commit       string
	Mode         comparisonBaseMode
}

type branchCreation struct {
	Commit  string
	Message string
}

type repoChanges struct {
	tracked   []string
	untracked []string
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
}

type packageIssue struct {
	Severity string `json:"severity"`
	Latent   bool   `json:"latent"`
	Message  string `json:"message"`
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
		sandboxAbsDir:   sandboxAbsDir,
		authorizer:      authorizer,
		agentInvoker:    option.AgentInvoker,
		model:           option.Model,
		maxConcurrency:  option.MaxConcurrency,
		git:             execGitRunner{},
		specDiffContext: computeSpecDiffContext,
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
				"description": "If true, only check packages whose on-disk state changed against the current git comparison base.",
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

	base, err := t.determineComparisonBase(ctx, mod.AbsolutePath)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	if err := mod.LoadAllPackages(); err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	changes, err := t.collectRepoChanges(ctx, mod.AbsolutePath, base.Commit)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	eligible, err := t.findEligiblePackages(mod, changes, params.OnlyChanged)
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

func (t *toolCheckSpecConformance) findEligiblePackages(mod *gocode.Module, changes repoChanges, onlyChanged bool) ([]eligiblePackage, error) {
	pkgs := make([]*gocode.Package, 0, len(mod.Packages))
	for _, pkg := range mod.Packages {
		pkgs = append(pkgs, pkg)
	}
	sort.Slice(pkgs, func(i, j int) bool {
		return packageResultKey(pkgs[i].RelativeDir) < packageResultKey(pkgs[j].RelativeDir)
	})

	eligible := make([]eligiblePackage, 0, len(pkgs))
	for _, pkg := range pkgs {
		specPath := filepath.Join(pkg.AbsolutePath(), "SPEC.md")
		if !pathExists(specPath) {
			continue
		}

		found, conforms, err := retrieveConformanceState(pkg)
		if err != nil {
			return nil, fmt.Errorf("retrieve CAS conformance for %s: %w", packageResultKey(pkg.RelativeDir), err)
		}
		if found && conforms {
			continue
		}

		hasDiff := changes.packageHasChanges(packageResultKey(pkg.RelativeDir))
		if onlyChanged && !hasDiff {
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
	packageDiff, err := t.buildPackageDiff(ctx, moduleAbsDir, pkg.Key, base.Commit, changes)
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

	if result.Conforms != nil && *result.Conforms {
		if err := t.storeConformanceState(pkg.Package); err != nil {
			return packageErrorResult(fmt.Errorf("store CAS conformance: %w", err))
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

	unit, err := codeunit.NewCodeUnit(fmt.Sprintf("package %s", req.Package.ImportPath), req.Package.AbsolutePath())
	if err != nil {
		return "", err
	}
	unit.IncludeEntireSubtree()

	var pkgAuthorizer authdomain.Authorizer
	if t.authorizer != nil {
		pkgAuthorizer = authdomain.NewCodeUnitAuthorizer(unit, t.authorizer)
	}

	agentCreator, err := subAgentCreatorFromContextSafe(ctx)
	if err != nil {
		return "", err
	}

	instructions := buildPackageCheckInstructions(req)
	events, err := t.agentInvoker.Invoke(ctx, checkSpecConformanceAgentName, toolsetinterface.InvokeRequest{
		AgentCreator:     agentCreator,
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

func buildPackageCheckInstructions(req packageCheckRequest) string {
	var body strings.Builder
	body.WriteString("Use the $spec-md check-conformance workflow for this package.\n")
	body.WriteString("This is read-only in intent. Do not modify files.\n")
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
	switch b.Mode {
	case comparisonBaseModeHEAD:
		return fmt.Sprintf("branch %s, compare on-disk state against HEAD %s", b.Branch, shortCommit(b.Commit))
	case comparisonBaseModeBranchPoint:
		return fmt.Sprintf("branch %s, parent branch %s, compare on-disk state against branch-point commit %s", b.Branch, b.ParentBranch, shortCommit(b.Commit))
	default:
		return shortCommit(b.Commit)
	}
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

	if branch == "main" || branch == "master" {
		headCommit, err := t.git.Output(ctx, repoAbsDir, "rev-parse", "HEAD")
		if err != nil {
			return comparisonBase{}, fmt.Errorf("resolve HEAD commit: %w", err)
		}
		return comparisonBase{
			Branch: branch,
			Commit: trimLineEndings(headCommit),
			Mode:   comparisonBaseModeHEAD,
		}, nil
	}

	created, err := t.oldestBranchCreation(ctx, repoAbsDir, branch)
	if err != nil {
		return comparisonBase{}, err
	}

	candidates, err := t.parentBranchCandidates(ctx, repoAbsDir, branch, created.Commit)
	if err != nil {
		return comparisonBase{}, err
	}

	if parent := parentBranchFromCreationMessage(created.Message, branch, candidates); parent != "" {
		return comparisonBase{
			Branch:       branch,
			ParentBranch: parent,
			Commit:       created.Commit,
			Mode:         comparisonBaseModeBranchPoint,
		}, nil
	}

	if len(candidates) == 1 {
		return comparisonBase{
			Branch:       branch,
			ParentBranch: candidates[0],
			Commit:       created.Commit,
			Mode:         comparisonBaseModeBranchPoint,
		}, nil
	}
	if len(candidates) == 0 {
		return comparisonBase{}, fmt.Errorf("unable to determine parent branch for %q at branch-point commit %s", branch, shortCommit(created.Commit))
	}
	return comparisonBase{}, fmt.Errorf("ambiguous parent branch for %q at branch-point commit %s: %s", branch, shortCommit(created.Commit), strings.Join(candidates, ", "))
}

func (t *toolCheckSpecConformance) oldestBranchCreation(ctx context.Context, repoAbsDir string, branch string) (branchCreation, error) {
	out, err := t.git.Output(ctx, repoAbsDir, "reflog", "show", "--format=%H%x00%gs", "refs/heads/"+branch)
	if err != nil {
		return branchCreation{}, fmt.Errorf("inspect branch reflog for %q: %w", branch, err)
	}

	lines := splitNonEmptyLines(out)
	if len(lines) == 0 {
		return branchCreation{}, fmt.Errorf("branch %q has no reflog history", branch)
	}

	parts := strings.SplitN(lines[len(lines)-1], "\x00", 2)
	creation := branchCreation{Commit: parts[0]}
	if len(parts) == 2 {
		creation.Message = parts[1]
	}
	if creation.Commit == "" {
		return branchCreation{}, fmt.Errorf("branch %q has an empty branch-point commit in its reflog", branch)
	}
	return creation, nil
}

func (t *toolCheckSpecConformance) parentBranchCandidates(ctx context.Context, repoAbsDir string, currentBranch string, commit string) ([]string, error) {
	out, err := t.git.Output(ctx, repoAbsDir, "branch", "--format=%(refname:short)", "--contains", commit)
	if err != nil {
		return nil, fmt.Errorf("find parent-branch candidates for %q: %w", currentBranch, err)
	}

	lines := splitNonEmptyLines(out)
	candidates := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimPrefix(line, "* ")
		if line == "" || line == currentBranch {
			continue
		}
		candidates = append(candidates, line)
	}
	sort.Strings(candidates)
	return candidates, nil
}

func parentBranchFromCreationMessage(message string, currentBranch string, candidates []string) string {
	const prefix = "branch: Created from "
	if !strings.HasPrefix(message, prefix) {
		return ""
	}

	parent := strings.TrimPrefix(message, prefix)
	parent = strings.TrimPrefix(parent, "refs/heads/")
	if parent == "" || parent == "HEAD" || parent == currentBranch {
		return ""
	}

	for _, candidate := range candidates {
		if candidate == parent {
			return parent
		}
	}
	return ""
}

func (t *toolCheckSpecConformance) collectRepoChanges(ctx context.Context, repoAbsDir string, baseCommit string) (repoChanges, error) {
	trackedOut, err := t.git.Output(ctx, repoAbsDir, "diff", "--name-only", "--relative", baseCommit, "--")
	if err != nil {
		return repoChanges{}, fmt.Errorf("collect tracked git changes: %w", err)
	}
	untrackedOut, err := t.git.Output(ctx, repoAbsDir, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return repoChanges{}, fmt.Errorf("collect untracked git changes: %w", err)
	}

	return repoChanges{
		tracked:   splitNonEmptyLines(trackedOut),
		untracked: splitNonEmptyLines(untrackedOut),
	}, nil
}

func (c repoChanges) packageHasChanges(pkgKey string) bool {
	for _, path := range c.tracked {
		if pathInPackage(path, pkgKey) {
			return true
		}
	}
	for _, path := range c.untracked {
		if pathInPackage(path, pkgKey) {
			return true
		}
	}
	return false
}

func (t *toolCheckSpecConformance) buildPackageDiff(ctx context.Context, repoAbsDir string, pkgKey string, baseCommit string, changes repoChanges) (string, error) {
	pathspec := pkgKey
	if pathspec == "." {
		pathspec = "."
	}

	trackedDiff, err := t.git.Output(ctx, repoAbsDir, "diff", "--no-ext-diff", "--relative", baseCommit, "--", pathspec)
	if err != nil {
		return "", err
	}

	var buf strings.Builder
	if trackedDiff != "" {
		buf.WriteString(trimTrailingNewline(trackedDiff))
	}

	for _, relPath := range changes.untracked {
		if !pathInPackage(relPath, pkgKey) {
			continue
		}
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

func pathInPackage(relPath string, pkgKey string) bool {
	if pkgKey == "." || pkgKey == "" {
		return true
	}
	return relPath == pkgKey || strings.HasPrefix(relPath, pkgKey+"/")
}

func packageResultKey(relativeDir string) string {
	if relativeDir == "" || relativeDir == "." {
		return "."
	}
	return filepath.ToSlash(relativeDir)
}

func parsePackageCheckResult(answer string, hasDiff bool) (packageCheckResult, error) {
	payload := extractJSONObject(answer)
	if payload == "" {
		return packageCheckResult{}, fmt.Errorf("subagent returned non-JSON result")
	}

	var result packageCheckResult
	decoder := json.NewDecoder(strings.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return packageCheckResult{}, fmt.Errorf("decode subagent JSON: %w", err)
	}

	if result.Error != "" {
		return packageCheckResult{}, fmt.Errorf("subagent returned unexpected error payload: %s", result.Error)
	}
	if result.Conforms == nil {
		return packageCheckResult{}, fmt.Errorf("subagent JSON must include conforms")
	}

	if *result.Conforms {
		result.Nonconformances = nil
		return result, nil
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
		if !hasDiff {
			result.Nonconformances[i].Latent = true
		}
	}

	return result, nil
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

func newCASDB(moduleAbsDir string) *gocas.DB {
	return &gocas.DB{
		BaseDir: moduleAbsDir,
		DB: cas.DB{
			AbsRoot: filepath.Join(moduleAbsDir, ".codalotl", "cas"),
		},
	}
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

func shortCommit(commit string) string {
	if len(commit) <= 12 {
		return commit
	}
	return commit[:12]
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

func (checkSpecConformancePresenter) SubagentEventPolicy(call llmstream.ToolCall) llmstream.SubagentEventPolicy {
	return llmstream.SubagentEventPolicyHideFinalMessage
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
	var results map[string]packageCheckResult
	if err := json.Unmarshal([]byte(raw), &results); err != nil {
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

	keys := make([]string, 0, len(results))
	for key := range results {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	conforming := make([]string, 0)
	nonconforming := make([]string, 0)
	errors := make([]string, 0)
	for _, key := range keys {
		result := results[key]
		if result.Error != "" {
			errors = append(errors, key)
			continue
		}
		if result.Conforms != nil && *result.Conforms {
			conforming = append(conforming, key)
			continue
		}
		nonconforming = append(nonconforming, fmt.Sprintf("%s (%d)", key, len(result.Nonconformances)))
	}

	lines := []llmstream.Line{{
		JoinWithSpace: true,
		Segments: []llmstream.Segment{
			{Text: fmt.Sprintf("%d conforming,", len(conforming)), Role: llmstream.RoleAccent},
			{Text: fmt.Sprintf("%d non-conforming,", len(nonconforming)), Role: llmstream.RoleAccent},
			{Text: fmt.Sprintf("%d errors", len(errors)), Role: llmstream.RoleAccent},
		},
	}}
	if len(conforming) > 0 {
		lines = append(lines, llmstream.Line{
			JoinWithSpace: true,
			Segments: []llmstream.Segment{
				{Text: "Conforming:", Role: llmstream.RoleAccent},
				{Text: strings.Join(conforming, ", "), Role: llmstream.RoleNormal},
			},
		})
	}
	if len(nonconforming) > 0 {
		lines = append(lines, llmstream.Line{
			JoinWithSpace: true,
			Segments: []llmstream.Segment{
				{Text: "Non-conforming:", Role: llmstream.RoleAccent},
				{Text: strings.Join(nonconforming, ", "), Role: llmstream.RoleNormal},
			},
		})
	}
	if len(errors) > 0 {
		lines = append(lines, llmstream.Line{
			JoinWithSpace: true,
			Segments: []llmstream.Segment{
				{Text: "Errors:", Role: llmstream.RoleAccent},
				{Text: strings.Join(errors, ", "), Role: llmstream.RoleNormal},
			},
		})
	}

	return llmstream.Paragraph{Lines: lines}, true
}
