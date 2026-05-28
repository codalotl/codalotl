package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/codalotl/codalotl/internal/gocode"
	qcli "github.com/codalotl/codalotl/internal/q/cli"
)

const prNewInitialTemplate = `# PR

## User Summary (do not modify)


`

var (
	prNewNow          = time.Now
	prNewSafeNameExpr = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	runPRNewGit       = runPRNewGitCommand
)

const (
	prRefactorDocsAdd            = "docs-add"
	prRefactorDocsFix            = "docs-fix"
	prRefactorDry                = "dry"
	prRefactorTestCleanup        = "test-cleanup"
	prRefactorTestEnsureCoverage = "test-ensure-coverage"
)

var supportedPRRefactors = []string{
	prRefactorDocsAdd,
	prRefactorDocsFix,
	prRefactorDry,
	prRefactorTestCleanup,
	prRefactorTestEnsureCoverage,
}

type prRefactorOptions struct {
	PackageArg   string
	AllPackages  bool
	RefactorName string
}

func newPRCommand() *qcli.Command {
	prCmd := &qcli.Command{
		Name:  "pr",
		Short: "PR orchestrator workflow tools.",
		Long:  "Commands for creating and managing local PR orchestrator workflow files.",
	}

	newCmd := &qcli.Command{
		Name:  "new",
		Short: "Create a PR orchestrator file and branch.",
		Long: "Creates an initial PR orchestrator file in .prs. By default it validates repo state, creates a feature branch, commits the PR file, " +
			"and pushes with upstream tracking when origin exists. Use --no-git to only create the file.",
		Usage: "<feature-name>",
		ArgHelp: []qcli.ArgHelp{
			{
				Display:     "<feature-name>",
				Description: "Filesystem-safe feature name used as the PR file suffix and branch name.",
			},
		},
		Example: strings.TrimSpace(`
codalotl pr new add-orchestrator-pr-new
codalotl pr new cas-prune --no-git
`),
	}
	noGit := newCmd.Flags().Bool("no-git", 0, false, "Only create the PR file; do not inspect or modify git state.")
	newCmd.Args = func(args []string) error {
		if err := qcli.ExactArgs(1)(args); err != nil {
			return err
		}
		return validatePRNewName(args[0], "<feature-name>")
	}
	newCmd.Run = func(c *qcli.Context) error {
		return runPRNew(c.Context, c.Out, c.Args[0], *noGit)
	}

	refactorCmd := &qcli.Command{
		Name:  "refactor",
		Short: "Create a package refactor PR orchestrator file and branch.",
		Long: "Creates a PR orchestrator file prefilled with package-refactor instructions. It validates repo state, creates a generated refactor branch, " +
			"commits the PR file, and pushes with upstream tracking when origin exists.",
		Usage: "(--package=<path/to/pkg> | --all-packages) [--refactor=<name>]",
		ArgHelp: []qcli.ArgHelp{
			{
				Display:     "--package=<path/to/pkg>",
				Description: packagePathArgDescription,
			},
			{
				Display:     "--all-packages",
				Description: "Target all Go packages in the current module. Requires --refactor.",
			},
			{
				Display:     "--refactor=<name>",
				Description: "Optional refactor flow: docs-add, docs-fix, dry, test-cleanup, or test-ensure-coverage.",
			},
		},
		Example: strings.TrimSpace(`
codalotl pr refactor --package=internal/mypkg
codalotl pr refactor --package=./internal/mypkg
codalotl pr refactor --package=internal/mypkg --refactor=docs-fix
codalotl pr refactor --all-packages --refactor=docs-fix
`),
	}
	refactorFlags := refactorCmd.Flags()
	refactorPackage := refactorFlags.String("package", 'p', "", "Package to refactor (import path or dir; must resolve to a single Go package).")
	refactorAllPackages := refactorFlags.Bool("all-packages", 0, false, "Target all Go packages in the current module. Requires --refactor.")
	refactorName := refactorFlags.String("refactor", 0, "", "Optional refactor flow: docs-add, docs-fix, dry, test-cleanup, or test-ensure-coverage.")
	refactorCmd.Args = func(args []string) error {
		if err := qcli.NoArgs(args); err != nil {
			return err
		}
		hasPackage := strings.TrimSpace(*refactorPackage) != ""
		if hasPackage == *refactorAllPackages {
			return qcli.UsageError{Message: "supply exactly one of --package or --all-packages"}
		}
		if *refactorAllPackages && strings.TrimSpace(*refactorName) == "" {
			return qcli.UsageError{Message: "--all-packages requires --refactor"}
		}
		if err := validatePRRefactorName(*refactorName); err != nil {
			return err
		}
		return nil
	}
	refactorCmd.Run = func(c *qcli.Context) error {
		return runPRRefactor(c.Context, c.Out, prRefactorOptions{
			PackageArg:   *refactorPackage,
			AllPackages:  *refactorAllPackages,
			RefactorName: *refactorName,
		})
	}

	prCmd.AddCommand(newCmd, refactorCmd)
	return prCmd
}

func runPRNew(ctx context.Context, out io.Writer, featureName string, noGit bool) error {
	return runPRScaffold(ctx, out, featureName, noGit, prNewInitialTemplate)
}

func runPRRefactor(ctx context.Context, out io.Writer, opts prRefactorOptions) error {
	refactorName := strings.TrimSpace(opts.RefactorName)
	if err := validatePRRefactorName(refactorName); err != nil {
		return err
	}

	if opts.AllPackages {
		featureName, err := prRefactorAllPackagesFeatureName(refactorName)
		if err != nil {
			return err
		}
		return runPRScaffold(ctx, out, featureName, false, prRefactorAllPackagesTemplate(refactorName))
	}

	pkg, _, err := loadPackageArg(opts.PackageArg)
	if err != nil {
		return err
	}
	packagePath := prRefactorPackagePath(pkg)
	featureName, err := prRefactorPackageFeatureName(packagePath, refactorName)
	if err != nil {
		return err
	}
	if refactorName != "" {
		return runPRScaffold(ctx, out, featureName, false, prRefactorSinglePackageTemplate(packagePath, refactorName))
	}
	return runPRScaffold(ctx, out, featureName, false, prRefactorAllPackageRefactorsTemplate(packagePath))
}

func runPRScaffold(ctx context.Context, out io.Writer, featureName string, noGit bool, content string) error {
	featureName = strings.TrimSpace(featureName)
	if err := validatePRNewName(featureName, "<feature-name>"); err != nil {
		return err
	}
	if content == "" {
		content = prNewInitialTemplate
	}

	baseDir, err := os.Getwd()
	if err != nil {
		return err
	}
	branchName := prNewBranchName(featureName)

	if !noGit {
		repoRoot, err := preparePRNewGit(ctx, baseDir, branchName)
		if err != nil {
			return qcli.ExitError{Code: 1, Err: err}
		}
		baseDir = repoRoot
	}

	relPath, absPath, err := createPRNewFile(baseDir, featureName, prNewNow(), content)
	if err != nil {
		return err
	}

	if !noGit {
		if err := finalizePRNewGit(ctx, baseDir, relPath, branchName, featureName); err != nil {
			return qcli.ExitError{Code: 1, Err: err}
		}
	}

	displayPath := filepath.ToSlash(relPath)
	if noGit {
		if rel, err := filepath.Rel(baseDir, absPath); err == nil {
			displayPath = filepath.ToSlash(rel)
		}
	}
	return writeStringln(out, fmt.Sprintf("Created %s", displayPath))
}

func prRefactorPackagePath(pkg *gocode.Package) string {
	rel := strings.TrimSpace(filepath.ToSlash(pkg.RelativeDir))
	if rel != "" && rel != "." {
		return rel
	}
	if importPath := strings.TrimSpace(pkg.ImportPath); importPath != "" {
		return filepath.ToSlash(importPath)
	}
	return filepath.ToSlash(pkg.AbsolutePath())
}

func prRefactorFeatureName(packagePath string) (string, error) {
	return prRefactorPackageFeatureName(packagePath, "")
}

func prRefactorPackageFeatureName(packagePath string, refactorName string) (string, error) {
	if strings.TrimSpace(refactorName) != "" {
		return prRefactorFeatureNameParts(refactorName, packagePath)
	}
	return prRefactorFeatureNameParts(packagePath)
}

func prRefactorAllPackagesFeatureName(refactorName string) (string, error) {
	if strings.TrimSpace(refactorName) == "" {
		return "", qcli.UsageError{Message: "--all-packages requires --refactor"}
	}
	return prRefactorFeatureNameParts(refactorName, "all-packages")
}

func prRefactorFeatureNameParts(parts ...string) (string, error) {
	joined := strings.Join(parts, "-")
	safePath := strings.Trim(strings.Map(func(r rune) rune {
		switch {
		case r >= 'A' && r <= 'Z':
			return r
		case r >= 'a' && r <= 'z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '.' || r == '_' || r == '-':
			return r
		case r == '/' || r == '\\':
			return '-'
		default:
			return '-'
		}
	}, filepath.ToSlash(strings.TrimSpace(joined))), ".-_")
	if safePath == "" {
		safePath = "package"
	}
	featureName := "refactor-" + collapseRepeatedHyphens(safePath)
	if err := validatePRNewName(featureName, "<feature-name>"); err != nil {
		return "", err
	}
	return featureName, nil
}

func collapseRepeatedHyphens(s string) string {
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return s
}

func validatePRRefactorName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	for _, supported := range supportedPRRefactors {
		if name == supported {
			return nil
		}
	}
	return qcli.UsageError{Message: fmt.Sprintf("unsupported --refactor %q (supported: %s)", name, strings.Join(supportedPRRefactors, ", "))}
}

func prRefactorCASNamespaces(refactorName string) string {
	switch strings.TrimSpace(refactorName) {
	case "":
		return "docs-fix,refactor-dry,refactor-test-cleanup,refactor-test-ensure-coverage"
	case prRefactorDocsFix:
		return "docs-fix"
	case prRefactorDry:
		return "refactor-dry"
	case prRefactorTestCleanup:
		return "refactor-test-cleanup"
	case prRefactorTestEnsureCoverage:
		return "refactor-test-ensure-coverage"
	default:
		return ""
	}
}

func prRefactorAllPackageRefactorsTemplate(packagePath string) string {
	const recertifyNamespaces = "docs-fix,refactor-dry,refactor-test-cleanup,refactor-test-ensure-coverage"

	return fmt.Sprintf(`# PR

## User Summary (do not modify)

In this PR, refactor %s.

Target package: %s
Selected refactor flow: all refactors for one package

Run these refactors in order:
1. refactor("name": "docs-add", "package": "%s")
2. refactor("name": "docs-fix", "package": "%s")
3. refactor("name": "dry", "package": "%s")
4. refactor("name": "test-cleanup", "package": "%s")
5. refactor("name": "test-ensure-coverage", "package": "%s")

Additional instructions:
- After each refactor, inspect the diff before continuing.
- If the diff looks good, commit that refactor separately. Include source changes and relevant CAS files in the commit.
- If a refactor result is a no-op, skip it with a note in this PR file.
- If the diff looks risky or outside scope, avoid risky fix-forward behavior. Revert, skip with a note in this PR file, or make only a minimal low-risk correction.
- These refactors are intended to be safe and low risk. Do not change public API or behavior except for documentation changes.
- After the final refactor is committed, use the codalotl_cli tool to run:
  codalotl cas recertify %s --namespaces="%s"
- Inspect and commit CAS files produced by recertify.

`, packagePath, packagePath, packagePath, packagePath, packagePath, packagePath, packagePath, packagePath, recertifyNamespaces)
}

func prRefactorSinglePackageTemplate(packagePath string, refactorName string) string {
	recertifyNamespaces := prRefactorCASNamespaces(refactorName)
	recertifyInstructions := "No CAS namespace is currently recertifiable specifically for this refactor. If accepted changes invalidate other applicable CAS records, recertify those after final changes."
	if recertifyNamespaces != "" {
		recertifyInstructions = fmt.Sprintf("After the final accepted changes, use the codalotl_cli tool to run:\n  codalotl cas recertify %s --namespaces=%q\n- Inspect and commit CAS files produced by recertify.", packagePath, recertifyNamespaces)
	}

	return fmt.Sprintf(`# PR

## User Summary (do not modify)

In this PR, run the %s refactor for %s.

Target package: %s
Selected refactor flow: %s

Run this refactor:
1. refactor("name": "%s", "package": "%s")

Additional instructions:
- Inspect the refactor diff before continuing.
- If the diff looks good, commit the accepted changes. Include source changes and relevant CAS files in the commit.
- If the diff is a no-op, looks risky, or is outside scope, skip it with a note in this PR file.
- These refactors are intended to be safe and low risk. Do not change public API or behavior except for documentation changes.
- %s

`, refactorName, packagePath, packagePath, refactorName, refactorName, packagePath, recertifyInstructions)
}

func prRefactorAllPackagesTemplate(refactorName string) string {
	recertifyNamespaces := prRefactorCASNamespaces(refactorName)
	recertifyInstructions := "No CAS namespace is currently recertifiable specifically for this refactor. If accepted package changes invalidate other applicable CAS records, recertify those after final changes."
	if recertifyNamespaces != "" {
		recertifyInstructions = fmt.Sprintf("After final accepted changes, use the codalotl_cli tool for each accepted package that needs recertification:\n  codalotl cas recertify <package> --namespaces=%q\n- Inspect and commit CAS files produced by recertify.", recertifyNamespaces)
	}

	return fmt.Sprintf(`# PR

## User Summary (do not modify)

In this PR, run the %s refactor across all Go packages in the current module.

Target: all Go packages in the current module
Selected refactor flow: %s

For each package in the current module:
1. refactor("name": "%s", "package": "<package>")

Additional instructions:
- Inspect each refactor result and diff before moving to the next package.
- Commit accepted changes with source changes and relevant CAS files. Prefer focused commits per package or small package group.
- Skip no-op packages without a commit.
- If a package looks risky or outside scope, do not fix-forward aggressively; revert/skip it and add a note in this PR file explaining why.
- Due to CAS, packages already up to date for this refactor may be no-ops.
- %s

`, refactorName, refactorName, refactorName, recertifyInstructions)
}

func validatePRNewName(name string, label string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return qcli.UsageError{Message: fmt.Sprintf("missing %s", label)}
	}
	if name == "." || name == ".." || strings.Contains(name, "..") || !prNewSafeNameExpr.MatchString(name) {
		return qcli.UsageError{Message: fmt.Sprintf("invalid %s: must start with a letter or digit and contain only letters, digits, '.', '_', or '-'", label)}
	}
	if strings.ContainsAny(name, `/\`) {
		return qcli.UsageError{Message: fmt.Sprintf("invalid %s: must not contain path separators", label)}
	}
	return nil
}

func prNewBranchName(featureName string) string {
	initials := strings.TrimSpace(os.Getenv("CODALOTL_USER_INITIALS"))
	if initials == "" {
		return featureName
	}
	return initials + "/" + featureName
}

func validatePRNewBranchComponent(component string, label string) error {
	if err := validatePRNewName(component, label); err != nil {
		return err
	}
	if strings.HasPrefix(component, "-") || strings.HasSuffix(component, ".") {
		return qcli.UsageError{Message: fmt.Sprintf("invalid %s for git branch name", label)}
	}
	return nil
}

func preparePRNewGit(ctx context.Context, cwd string, branchName string) (string, error) {
	if strings.Contains(branchName, "/") {
		parts := strings.Split(branchName, "/")
		if len(parts) != 2 {
			return "", qcli.UsageError{Message: "invalid git branch name"}
		}
		if err := validatePRNewBranchComponent(parts[0], "CODALOTL_USER_INITIALS"); err != nil {
			return "", err
		}
		if err := validatePRNewBranchComponent(parts[1], "<feature-name>"); err != nil {
			return "", err
		}
	} else if err := validatePRNewBranchComponent(branchName, "<feature-name>"); err != nil {
		return "", err
	}

	repoRoot, err := gitOutput(ctx, cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	repoRoot = strings.TrimSpace(repoRoot)
	if repoRoot == "" {
		return "", fmt.Errorf("git rev-parse --show-toplevel returned empty repo root")
	}

	status, err := gitOutput(ctx, repoRoot, "status", "--porcelain")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(status) != "" {
		return "", fmt.Errorf("working tree is not clean")
	}

	currentBranch, err := gitOutput(ctx, repoRoot, "branch", "--show-current")
	if err != nil {
		return "", err
	}
	currentBranch = strings.TrimSpace(currentBranch)
	if currentBranch != "main" && currentBranch != "master" {
		return "", fmt.Errorf("current branch must be main or master (got %q)", currentBranch)
	}

	upstream, err := gitOutput(ctx, repoRoot, "for-each-ref", "--format=%(upstream:short)", "refs/heads/"+currentBranch)
	if err != nil {
		return "", fmt.Errorf("could not determine upstream status: %w", err)
	}
	if strings.TrimSpace(upstream) != "" {
		counts, err := gitOutput(ctx, repoRoot, "rev-list", "--left-right", "--count", "HEAD...@{u}")
		if err != nil {
			return "", fmt.Errorf("could not determine upstream status: %w", err)
		}
		fields := strings.Fields(counts)
		if len(fields) != 2 {
			return "", fmt.Errorf("could not determine upstream status: %q", strings.TrimSpace(counts))
		}
		if fields[0] != "0" || fields[1] != "0" {
			return "", fmt.Errorf("current branch is not up to date with upstream (ahead %s, behind %s)", fields[0], fields[1])
		}
	}

	if _, err := gitOutput(ctx, repoRoot, "checkout", "-b", branchName); err != nil {
		return "", err
	}
	return repoRoot, nil
}

func createPRNewFile(baseDir string, featureName string, now time.Time, content string) (string, string, error) {
	filename := fmt.Sprintf("%s_%d_%s.md", now.Format("2006-01-02"), now.Unix(), featureName)
	relPath := filepath.Join(".prs", filename)
	absPath := filepath.Join(baseDir, relPath)

	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		return "", "", err
	}

	f, err := os.OpenFile(absPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		if os.IsExist(err) {
			return "", "", fmt.Errorf("PR file already exists: %s", relPath)
		}
		return "", "", err
	}

	if _, err := io.WriteString(f, content); err != nil {
		_ = f.Close()
		return "", "", err
	}
	if err := f.Close(); err != nil {
		return "", "", err
	}
	return relPath, absPath, nil
}

func finalizePRNewGit(ctx context.Context, repoRoot string, relPath string, branchName string, featureName string) error {
	gitRelPath := filepath.ToSlash(relPath)
	if _, err := gitOutput(ctx, repoRoot, "add", gitRelPath); err != nil {
		return err
	}
	if _, err := gitOutput(ctx, repoRoot, "commit", "-m", "Add PR file for "+featureName); err != nil {
		return err
	}
	if _, err := gitOutput(ctx, repoRoot, "remote", "get-url", "origin"); err != nil {
		return nil
	}
	_, err := gitOutput(ctx, repoRoot, "push", "-u", "origin", branchName)
	return err
}

func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	return runPRNewGit(ctx, dir, args...)
}

func runPRNewGitCommand(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil {
		return stdout.String(), nil
	}

	msg := strings.TrimSpace(stderr.String())
	if msg == "" {
		msg = strings.TrimSpace(stdout.String())
	}
	if msg != "" {
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
}
