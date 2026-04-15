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

type branchRef struct {
	Name   string
	Remote bool
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
}

type packageIssue struct {
	Severity string `json:"severity"`
	Latent   bool   `json:"latent"`
	Message  string `json:"message"`
}

type parentBranchChoice struct {
	AliasKey     string
	ParentBranch string
	ForkPoint    string
	Aliases      []string
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
				"description": "If true, only check packages whose state changed (in feature branches, compares on-disk state to branch merge base).",
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

	pkgs, err := loadCurrentModulePackages(ctx, mod)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	changes, err := t.collectRepoChanges(ctx, mod.AbsolutePath, base.Commit)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	eligible, err := t.findEligiblePackages(pkgs, changes, params.OnlyChanged)
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

func (t *toolCheckSpecConformance) findEligiblePackages(pkgs []*gocode.Package, changes repoChanges, onlyChanged bool) ([]eligiblePackage, error) {
	sort.Slice(pkgs, func(i, j int) bool {
		return packageResultKey(pkgs[i].RelativeDir) < packageResultKey(pkgs[j].RelativeDir)
	})

	eligible := make([]eligiblePackage, 0, len(pkgs))
	for _, pkg := range pkgs {
		specPath := filepath.Join(pkg.AbsolutePath(), "SPEC.md")
		if !pathExists(specPath) {
			continue
		}

		hasDiff, err := packageHasChanges(pkg, changes)
		if err != nil {
			return nil, fmt.Errorf("check package diff scope for %s: %w", packageResultKey(pkg.RelativeDir), err)
		}

		found, conforms, err := retrieveConformanceState(pkg)
		if err != nil {
			return nil, fmt.Errorf("retrieve CAS conformance for %s: %w", packageResultKey(pkg.RelativeDir), err)
		}
		if found && conforms && !hasDiff {
			continue
		}

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

	unit, err := newPackageModeCodeUnit(fmt.Sprintf("package %s", req.Package.ImportPath), req.Package.AbsolutePath())
	if err != nil {
		return "", err
	}

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

	historyParents, err := t.parentBranchesFromCreationHistory(ctx, repoAbsDir, branch, created)
	if err != nil {
		return comparisonBase{}, err
	}

	candidates, err := t.parentBranchCandidates(ctx, repoAbsDir, branch, created.Commit)
	if err != nil {
		return comparisonBase{}, err
	}

	parent, err := t.selectParentBranch(ctx, repoAbsDir, branch, created.Commit, historyParents, candidates)
	if err != nil {
		return comparisonBase{}, err
	}

	return comparisonBase{
		Branch:       branch,
		ParentBranch: parent.ParentBranch,
		Commit:       parent.ForkPoint,
		Mode:         comparisonBaseModeBranchPoint,
	}, nil
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

func (t *toolCheckSpecConformance) parentBranchCandidates(ctx context.Context, repoAbsDir string, currentBranch string, commit string) ([]branchRef, error) {
	_ = commit

	localOut, err := t.git.Output(ctx, repoAbsDir, "branch", "--format=%(refname:short)")
	if err != nil {
		return nil, fmt.Errorf("find parent-branch candidates for %q: %w", currentBranch, err)
	}
	remoteOut, err := t.git.Output(ctx, repoAbsDir, "branch", "-r", "--format=%(refname:short)")
	if err != nil {
		return nil, fmt.Errorf("find parent-branch candidates for %q: %w", currentBranch, err)
	}

	localRefs := collectBranchRefs(splitNonEmptyLines(localOut), false, currentBranch)
	remoteRefs := collectBranchRefs(splitNonEmptyLines(remoteOut), true, currentBranch)
	candidates := append(localRefs, remoteRefs...)
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Name == candidates[j].Name {
			return !candidates[i].Remote && candidates[j].Remote
		}
		return candidates[i].Name < candidates[j].Name
	})
	return candidates, nil
}

func collectBranchRefs(lines []string, remote bool, currentBranch string) []branchRef {
	refs := make([]branchRef, 0, len(lines))
	seen := make(map[string]struct{}, len(lines))
	for _, line := range lines {
		name := strings.TrimSpace(strings.TrimPrefix(line, "* "))
		if name == "" || name == "HEAD" || strings.HasSuffix(name, "/HEAD") {
			continue
		}
		ref := branchRef{Name: name, Remote: remote}
		if isSelfBranchRef(ref, currentBranch) {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		refs = append(refs, ref)
	}
	return refs
}

func isSelfBranchRef(ref branchRef, currentBranch string) bool {
	if ref.Name == currentBranch {
		return true
	}
	return ref.Remote && branchRefAliasKey(ref) == currentBranch
}

func branchRefAliasKey(ref branchRef) string {
	if ref.Remote {
		if trimmed := trimRemoteTrackingBranch(ref.Name); trimmed != "" {
			return trimmed
		}
	}
	return ref.Name
}

func (t *toolCheckSpecConformance) parentBranchesFromCreationHistory(ctx context.Context, repoAbsDir string, currentBranch string, created branchCreation) ([]string, error) {
	const prefix = "branch: Created from "
	if !strings.HasPrefix(created.Message, prefix) {
		return nil, nil
	}

	parent := strings.TrimSpace(strings.TrimPrefix(created.Message, prefix))
	if parent == "" {
		return nil, nil
	}
	if parent != "HEAD" {
		return normalizeCreationMessageParentBranches(parent), nil
	}

	parents, err := t.parentBranchesFromHEADReflog(ctx, repoAbsDir, currentBranch, created.Commit)
	if err != nil {
		return nil, err
	}
	if len(parents) == 0 {
		return nil, fmt.Errorf("unable to determine parent branch for %q at branch-point commit %s from HEAD reflog", currentBranch, shortCommit(created.Commit))
	}
	return parents, nil
}

func (t *toolCheckSpecConformance) parentBranchesFromHEADReflog(ctx context.Context, repoAbsDir string, currentBranch string, commit string) ([]string, error) {
	out, err := t.git.Output(ctx, repoAbsDir, "reflog", "show", "--format=%H%x00%gs", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("inspect HEAD reflog for %q: %w", currentBranch, err)
	}

	lines := splitNonEmptyLines(out)
	for i := len(lines) - 1; i >= 0; i-- {
		parts := strings.SplitN(lines[i], "\x00", 2)
		if len(parts) != 2 || parts[0] != commit {
			continue
		}
		if parent := checkoutSourceBranch(parts[1], currentBranch); parent != "" {
			return normalizeCreationMessageParentBranches(parent), nil
		}
	}

	return nil, nil
}

func checkoutSourceBranch(message string, currentBranch string) string {
	const prefix = "checkout: moving from "
	suffix := " to " + currentBranch
	if !strings.HasPrefix(message, prefix) || !strings.HasSuffix(message, suffix) {
		return ""
	}
	return strings.TrimSuffix(strings.TrimPrefix(message, prefix), suffix)
}

func (t *toolCheckSpecConformance) selectParentBranch(ctx context.Context, repoAbsDir string, currentBranch string, branchPointCommit string, historyParents []string, refs []branchRef) (parentBranchChoice, error) {
	choices, err := t.parentBranchChoices(ctx, repoAbsDir, currentBranch, refs)
	if err != nil {
		return parentBranchChoice{}, err
	}
	if len(choices) == 0 {
		return parentBranchChoice{}, fmt.Errorf("unable to determine parent branch for %q at branch-point commit %s", currentBranch, shortCommit(branchPointCommit))
	}

	if len(historyParents) > 0 {
		matched := make([]parentBranchChoice, 0, len(choices))
		for _, choice := range choices {
			if parentBranchChoiceMatchesHistory(choice, historyParents) {
				matched = append(matched, choice)
			}
		}
		switch len(matched) {
		case 0:
			return parentBranchChoice{}, fmt.Errorf("unable to determine parent branch for %q at branch-point commit %s", currentBranch, shortCommit(branchPointCommit))
		case 1:
			return matched[0], nil
		default:
			return parentBranchChoice{}, fmt.Errorf("ambiguous parent branch for %q at branch-point commit %s: %s", currentBranch, shortCommit(branchPointCommit), strings.Join(parentBranchChoiceNames(matched), ", "))
		}
	}

	if len(choices) == 1 {
		return choices[0], nil
	}

	choice, ok, err := t.mostSpecificParentBranchChoice(ctx, repoAbsDir, choices)
	if err != nil {
		return parentBranchChoice{}, err
	}
	if ok {
		return choice, nil
	}

	return parentBranchChoice{}, fmt.Errorf("ambiguous parent branch for %q at branch-point commit %s: %s", currentBranch, shortCommit(branchPointCommit), strings.Join(parentBranchChoiceNames(choices), ", "))
}

func parentBranchChoiceMatchesHistory(choice parentBranchChoice, historyParents []string) bool {
	seen := make(map[string]struct{}, len(historyParents))
	for _, parent := range historyParents {
		seen[parent] = struct{}{}
	}
	if _, ok := seen[choice.ParentBranch]; ok {
		return true
	}
	if _, ok := seen[choice.AliasKey]; ok {
		return true
	}
	for _, alias := range choice.Aliases {
		if _, ok := seen[alias]; ok {
			return true
		}
	}
	return false
}

func parentBranchChoiceNames(choices []parentBranchChoice) []string {
	names := make([]string, 0, len(choices))
	for _, choice := range choices {
		names = append(names, choice.ParentBranch)
	}
	sort.Strings(names)
	return names
}

func (t *toolCheckSpecConformance) parentBranchChoices(ctx context.Context, repoAbsDir string, currentBranch string, refs []branchRef) ([]parentBranchChoice, error) {
	type choiceBuilder struct {
		choice            parentBranchChoice
		representativeRef branchRef
		aliases           map[string]struct{}
	}

	builders := make(map[string]*choiceBuilder, len(refs))
	for _, ref := range refs {
		forkPoint, err := t.currentForkPoint(ctx, repoAbsDir, ref.Name, currentBranch)
		if err != nil {
			continue
		}

		aliasKey := branchRefAliasKey(ref)
		builder, ok := builders[aliasKey]
		if !ok {
			builders[aliasKey] = &choiceBuilder{
				choice: parentBranchChoice{
					AliasKey:     aliasKey,
					ParentBranch: ref.Name,
					ForkPoint:    forkPoint,
				},
				representativeRef: ref,
				aliases:           map[string]struct{}{ref.Name: {}},
			}
			continue
		}

		builder.aliases[ref.Name] = struct{}{}
		newerForkPoint, err := t.newerForkPoint(ctx, repoAbsDir, builder.choice.ForkPoint, forkPoint)
		if err != nil {
			return nil, fmt.Errorf("resolve fork-point for %q aliases: %w", aliasKey, err)
		}
		if newerForkPoint == forkPoint && builder.choice.ForkPoint != forkPoint {
			builder.choice.ParentBranch = ref.Name
			builder.choice.ForkPoint = forkPoint
			builder.representativeRef = ref
			continue
		}
		if newerForkPoint == forkPoint && builder.choice.ForkPoint == forkPoint && builder.representativeRef.Remote && !ref.Remote {
			builder.choice.ParentBranch = ref.Name
			builder.representativeRef = ref
		}
	}

	choices := make([]parentBranchChoice, 0, len(builders))
	for _, builder := range builders {
		for alias := range builder.aliases {
			builder.choice.Aliases = append(builder.choice.Aliases, alias)
		}
		sort.Strings(builder.choice.Aliases)
		choices = append(choices, builder.choice)
	}
	sort.Slice(choices, func(i, j int) bool {
		return choices[i].ParentBranch < choices[j].ParentBranch
	})
	return choices, nil
}

func (t *toolCheckSpecConformance) currentForkPoint(ctx context.Context, repoAbsDir string, parentBranch string, currentBranch string) (string, error) {
	out, err := t.git.Output(ctx, repoAbsDir, "merge-base", "--fork-point", parentBranch, currentBranch)
	if err == nil {
		if commit := trimLineEndings(out); commit != "" {
			return commit, nil
		}
	}

	out, err = t.git.Output(ctx, repoAbsDir, "merge-base", parentBranch, currentBranch)
	if err != nil {
		return "", err
	}
	commit := trimLineEndings(out)
	if commit == "" {
		return "", fmt.Errorf("empty merge-base for %q and %q", parentBranch, currentBranch)
	}
	return commit, nil
}

func (t *toolCheckSpecConformance) newerForkPoint(ctx context.Context, repoAbsDir string, left string, right string) (string, error) {
	if left == right {
		return left, nil
	}

	rightDescendsFromLeft, err := t.commitIsAncestor(ctx, repoAbsDir, left, right)
	if err != nil {
		return "", err
	}
	if rightDescendsFromLeft {
		return right, nil
	}

	leftDescendsFromRight, err := t.commitIsAncestor(ctx, repoAbsDir, right, left)
	if err != nil {
		return "", err
	}
	if leftDescendsFromRight {
		return left, nil
	}

	return "", fmt.Errorf("incomparable fork-point commits %s and %s", shortCommit(left), shortCommit(right))
}

func (t *toolCheckSpecConformance) mostSpecificParentBranchChoice(ctx context.Context, repoAbsDir string, choices []parentBranchChoice) (parentBranchChoice, bool, error) {
	winners := make([]parentBranchChoice, 0, len(choices))
	for i, candidate := range choices {
		mostSpecific := true
		for j, other := range choices {
			if i == j {
				continue
			}
			descends, err := t.commitIsAncestor(ctx, repoAbsDir, other.ForkPoint, candidate.ForkPoint)
			if err != nil {
				return parentBranchChoice{}, false, err
			}
			if !descends {
				mostSpecific = false
				break
			}
		}
		if mostSpecific {
			winners = append(winners, candidate)
		}
	}

	if len(winners) != 1 {
		return parentBranchChoice{}, false, nil
	}
	return winners[0], true, nil
}

func (t *toolCheckSpecConformance) commitIsAncestor(ctx context.Context, repoAbsDir string, ancestor string, descendant string) (bool, error) {
	if ancestor == descendant {
		return true, nil
	}

	out, err := t.git.Output(ctx, repoAbsDir, "rev-list", "--ancestry-path", "--max-count=1", ancestor+".."+descendant)
	if err != nil {
		return false, err
	}
	return trimLineEndings(out) != "", nil
}

func parentBranchFromCreationMessage(message string, currentBranch string, candidates []string) string {
	const prefix = "branch: Created from "
	if !strings.HasPrefix(message, prefix) {
		return ""
	}

	parent := strings.TrimPrefix(message, prefix)
	normalized := normalizeCreationMessageParentBranches(parent)
	if len(normalized) == 0 {
		return ""
	}

	for _, candidate := range candidates {
		for _, parent := range normalized {
			if candidate == parent && candidate != currentBranch {
				return candidate
			}
		}
	}
	return ""
}

func normalizeCreationMessageParentBranches(parent string) []string {
	parent = strings.TrimSpace(parent)
	if parent == "" || parent == "HEAD" {
		return nil
	}

	var normalized []string
	seen := make(map[string]struct{})
	add := func(branch string) {
		branch = strings.TrimSpace(branch)
		if branch == "" || branch == "HEAD" {
			return
		}
		if _, ok := seen[branch]; ok {
			return
		}
		seen[branch] = struct{}{}
		normalized = append(normalized, branch)
	}

	add(parent)
	add(strings.TrimPrefix(parent, "refs/heads/"))

	for _, prefix := range []string{"refs/remotes/", "remotes/"} {
		if strings.HasPrefix(parent, prefix) {
			remoteRef := strings.TrimPrefix(parent, prefix)
			add(remoteRef)
			add(trimRemoteTrackingBranch(remoteRef))
		}
	}

	return normalized
}

func trimRemoteTrackingBranch(branch string) string {
	slash := strings.IndexByte(branch, '/')
	if slash <= 0 || slash == len(branch)-1 {
		return ""
	}
	return branch[slash+1:]
}

func (t *toolCheckSpecConformance) collectRepoChanges(ctx context.Context, repoAbsDir string, baseCommit string) (repoChanges, error) {
	trackedOut, err := t.git.Output(ctx, repoAbsDir, "diff", "--name-status", "--find-renames", "--relative", baseCommit, "--")
	if err != nil {
		return repoChanges{}, fmt.Errorf("collect tracked git changes: %w", err)
	}
	untrackedOut, err := t.git.Output(ctx, repoAbsDir, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return repoChanges{}, fmt.Errorf("collect untracked git changes: %w", err)
	}

	return repoChanges{
		tracked:   trackedChangePaths(trackedOut),
		untracked: splitNonEmptyLines(untrackedOut),
	}, nil
}

func trackedChangePaths(nameStatusOutput string) []string {
	lines := splitNonEmptyLines(nameStatusOutput)
	if len(lines) == 0 {
		return nil
	}

	paths := make([]string, 0, len(lines))
	seen := make(map[string]struct{}, len(lines))
	for _, line := range lines {
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			continue
		}
		for _, path := range fields[1:] {
			path = strings.TrimSpace(path)
			if path == "" {
				continue
			}
			if _, ok := seen[path]; ok {
				continue
			}
			seen[path] = struct{}{}
			paths = append(paths, path)
		}
	}
	return paths
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
	codeUnitScope, err := newPackageModeCodeUnit(fmt.Sprintf("package %s", pkg.ImportPath), pkg.AbsolutePath())
	if err != nil {
		return nil, err
	}

	blockedSubtrees, err := descendantPackageDirsOnDisk(pkg)
	if err != nil {
		return nil, err
	}
	blockedSubtrees = append(blockedSubtrees, descendantPackageDirsFromTrackedGoChanges(pkg, changes.tracked)...)

	return &packagePathScope{
		codeUnit:        codeUnitScope,
		moduleAbsDir:    pkg.Module.AbsolutePath,
		packageRelDir:   normalizeRelativeDir(pkg.RelativeDir),
		blockedSubtrees: compactRelativeDirs(blockedSubtrees),
	}, nil
}

func newPackageModeCodeUnit(name string, baseDir string) (*codeunit.CodeUnit, error) {
	unit, err := codeunit.NewCodeUnit(name, baseDir)
	if err != nil {
		return nil, err
	}
	if err := unit.IncludeSubtreeUnlessContains("*.go"); err != nil {
		return nil, err
	}
	if err := includeReachableTestdataDirs(unit, baseDir); err != nil {
		return nil, err
	}
	return unit, nil
}

func includeReachableTestdataDirs(unit *codeunit.CodeUnit, baseDir string) error {
	return filepath.WalkDir(baseDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() || path == baseDir || filepath.Base(path) != "testdata" {
			return nil
		}

		parentDir := filepath.Dir(path)
		if !unit.Includes(parentDir) {
			return filepath.SkipDir
		}
		if unit.Includes(path) {
			return nil
		}
		return unit.IncludeDir(path, true)
	})
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
