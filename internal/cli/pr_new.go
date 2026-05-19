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

	prCmd.AddCommand(newCmd)
	return prCmd
}

func runPRNew(ctx context.Context, out io.Writer, featureName string, noGit bool) error {
	featureName = strings.TrimSpace(featureName)
	if err := validatePRNewName(featureName, "<feature-name>"); err != nil {
		return err
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

	relPath, absPath, err := createPRNewFile(baseDir, featureName, prNewNow())
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

func createPRNewFile(baseDir string, featureName string, now time.Time) (string, string, error) {
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

	if _, err := io.WriteString(f, prNewInitialTemplate); err != nil {
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
