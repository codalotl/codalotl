package gittools

import (
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

// HeuristicMergeBase returns a best-effort base commit/ref for isolating commits on the current line of work.
func HeuristicMergeBase(repoDir string) (commit string, ref string, err error) {
	repoDir, err = repoRoot(repoDir)
	if err != nil {
		return "", "", err
	}

	currentBranch, _ := gitOutput(repoDir, "symbolic-ref", "--quiet", "--short", "HEAD")
	currentBranch = strings.TrimSpace(currentBranch)

	currentUpstream, _ := gitOutput(repoDir, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	currentUpstream = strings.TrimSpace(currentUpstream)

	defaultRefs := defaultRefSet(repoDir)

	candidates, err := candidateRefs(repoDir, currentBranch, currentUpstream, defaultRefs)
	if err != nil {
		return "", "", err
	}
	if len(candidates) == 0 {
		return "", "", fmt.Errorf("no plausible base refs found")
	}

	evaluated := make([]candidateScore, 0, len(candidates))
	for _, candidate := range candidates {
		score, ok := scoreCandidate(repoDir, candidate)
		if !ok {
			continue
		}
		evaluated = append(evaluated, score)
	}
	if len(evaluated) == 0 {
		return "", "", fmt.Errorf("no plausible base refs found")
	}

	sort.SliceStable(evaluated, func(i, j int) bool {
		return compareScores(evaluated[i], evaluated[j]) < 0
	})

	best := evaluated[0]
	bestRef := best.candidate.displayName
	if len(evaluated) > 1 {
		next := evaluated[1]
		if equivalentScore(best, next) && best.candidate.logicalKey != next.candidate.logicalKey {
			bestRef = ""
		}
	}

	return best.mergeBase, bestRef, nil
}

// ChangedPathsSince returns sorted unique repo-relative paths changed since baseCommit.
func ChangedPathsSince(repoDir string, baseCommit string, includeUncommitted bool) ([]string, error) {
	repoDir, err := repoRoot(repoDir)
	if err != nil {
		return nil, err
	}
	if baseCommit == "" {
		return nil, fmt.Errorf("base commit is required")
	}

	pathSet := map[string]struct{}{}

	if err := addChangedPathsFromDiff(repoDir, pathSet, baseCommit, "HEAD"); err != nil {
		return nil, err
	}
	if includeUncommitted {
		if err := addChangedPathsFromDiff(repoDir, pathSet, "HEAD"); err != nil {
			return nil, err
		}
		if err := addPathsFromNullOutput(repoDir, pathSet, "ls-files", "--others", "--exclude-standard", "-z"); err != nil {
			return nil, err
		}
	}

	paths := make([]string, 0, len(pathSet))
	for path := range pathSet {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}

type candidateRef struct {
	gitRef      string
	displayName string
	logicalKey  string
	isLocal     bool
	isDefault   bool
}

type candidateScore struct {
	candidate          candidateRef
	mergeBase          string
	mergeBaseUnixTime  int64
	headOnlyCount      int
	candidateOnlyCount int
	candidateAncestor  bool
}

func repoRoot(repoDir string) (string, error) {
	if repoDir == "" {
		repoDir = "."
	}

	out, err := gitOutput(repoDir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(out), nil
}

func candidateRefs(repoDir, currentBranch, currentUpstream string, defaultRefs map[string]bool) ([]candidateRef, error) {
	locals, err := gitLines(repoDir, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if err != nil {
		return nil, err
	}

	localSet := make(map[string]bool, len(locals))
	candidates := make([]candidateRef, 0, len(locals))
	for _, local := range locals {
		if local == currentBranch {
			continue
		}
		localSet[local] = true
		candidates = append(candidates, candidateRef{
			gitRef:      local,
			displayName: local,
			logicalKey:  "local:" + local,
			isLocal:     true,
			isDefault:   defaultRefs[local],
		})
	}

	remotes, err := gitLines(repoDir, "for-each-ref", "--format=%(refname:short)", "refs/remotes")
	if err != nil {
		return nil, err
	}

	for _, remote := range remotes {
		if strings.HasSuffix(remote, "/HEAD") {
			continue
		}
		if remote == currentUpstream {
			continue
		}

		shortName := remoteBranchName(remote)
		if shortName != "" && shortName == currentBranch {
			continue
		}
		if shortName != "" && localSet[shortName] {
			continue
		}

		candidates = append(candidates, candidateRef{
			gitRef:      remote,
			displayName: remote,
			logicalKey:  "remote:" + remote,
			isDefault:   defaultRefs[remote],
		})
	}

	return candidates, nil
}

func defaultRefSet(repoDir string) map[string]bool {
	defaults := map[string]bool{
		"main":   true,
		"master": true,
		"trunk":  true,
	}

	if out, err := gitOutput(repoDir, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil {
		defaults[strings.TrimSpace(out)] = true
	}

	return defaults
}

func scoreCandidate(repoDir string, candidate candidateRef) (candidateScore, bool) {
	mergeBase, err := gitOutput(repoDir, "merge-base", "HEAD", candidate.gitRef)
	if err != nil {
		return candidateScore{}, false
	}
	mergeBase = strings.TrimSpace(mergeBase)
	if mergeBase == "" {
		return candidateScore{}, false
	}

	headOnlyCount, err := revListCount(repoDir, mergeBase+"..HEAD")
	if err != nil {
		return candidateScore{}, false
	}

	candidateOnlyCount, err := revListCount(repoDir, mergeBase+".."+candidate.gitRef)
	if err != nil {
		return candidateScore{}, false
	}

	mergeBaseUnixTime, err := commitUnixTime(repoDir, mergeBase)
	if err != nil {
		return candidateScore{}, false
	}

	candidateAncestor := gitSuccess(repoDir, "merge-base", "--is-ancestor", candidate.gitRef, "HEAD")

	return candidateScore{
		candidate:          candidate,
		mergeBase:          mergeBase,
		mergeBaseUnixTime:  mergeBaseUnixTime,
		headOnlyCount:      headOnlyCount,
		candidateOnlyCount: candidateOnlyCount,
		candidateAncestor:  candidateAncestor,
	}, true
}

func compareScores(a, b candidateScore) int {
	if a.candidateAncestor != b.candidateAncestor {
		if a.candidateAncestor {
			return -1
		}
		return 1
	}

	if a.mergeBaseUnixTime != b.mergeBaseUnixTime {
		if a.mergeBaseUnixTime > b.mergeBaseUnixTime {
			return -1
		}
		return 1
	}

	if a.candidate.isDefault != b.candidate.isDefault {
		if a.candidate.isDefault {
			return -1
		}
		return 1
	}

	if a.candidate.isLocal != b.candidate.isLocal {
		if a.candidate.isLocal {
			return -1
		}
		return 1
	}

	if a.headOnlyCount != b.headOnlyCount {
		if a.headOnlyCount < b.headOnlyCount {
			return -1
		}
		return 1
	}

	if a.candidateOnlyCount != b.candidateOnlyCount {
		if a.candidateOnlyCount < b.candidateOnlyCount {
			return -1
		}
		return 1
	}

	return strings.Compare(a.candidate.gitRef, b.candidate.gitRef)
}

func equivalentScore(a, b candidateScore) bool {
	return a.candidateAncestor == b.candidateAncestor &&
		a.mergeBaseUnixTime == b.mergeBaseUnixTime &&
		a.candidate.isDefault == b.candidate.isDefault &&
		a.candidate.isLocal == b.candidate.isLocal &&
		a.headOnlyCount == b.headOnlyCount &&
		a.candidateOnlyCount == b.candidateOnlyCount
}

func revListCount(repoDir, revRange string) (int, error) {
	out, err := gitOutput(repoDir, "rev-list", "--count", revRange)
	if err != nil {
		return 0, err
	}

	count, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("parse git rev-list count %q: %w", strings.TrimSpace(out), err)
	}

	return count, nil
}

func commitUnixTime(repoDir, rev string) (int64, error) {
	out, err := gitOutput(repoDir, "show", "-s", "--format=%ct", rev)
	if err != nil {
		return 0, err
	}

	unixTime, err := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse git commit time %q: %w", strings.TrimSpace(out), err)
	}

	return unixTime, nil
}

func addChangedPathsFromDiff(repoDir string, pathSet map[string]struct{}, revs ...string) error {
	args := []string{"diff", "--name-status", "-z", "--find-renames"}
	args = append(args, revs...)
	args = append(args, "--")

	out, err := gitOutput(repoDir, args...)
	if err != nil {
		return err
	}

	fields := nullFields(out)
	for i := 0; i < len(fields); {
		status := fields[i]
		i++
		if status == "" {
			return fmt.Errorf("parse git diff output: empty status")
		}

		switch status[0] {
		case 'R', 'C':
			if i+1 >= len(fields) {
				return fmt.Errorf("parse git diff output: expected two paths for status %q", status)
			}
			pathSet[fields[i]] = struct{}{}
			pathSet[fields[i+1]] = struct{}{}
			i += 2
		default:
			if i >= len(fields) {
				return fmt.Errorf("parse git diff output: expected path for status %q", status)
			}
			pathSet[fields[i]] = struct{}{}
			i++
		}
	}

	return nil
}

func addPathsFromNullOutput(repoDir string, pathSet map[string]struct{}, args ...string) error {
	out, err := gitOutput(repoDir, args...)
	if err != nil {
		return err
	}

	for _, path := range nullFields(out) {
		pathSet[path] = struct{}{}
	}

	return nil
}

func gitLines(repoDir string, args ...string) ([]string, error) {
	out, err := gitOutput(repoDir, args...)
	if err != nil {
		return nil, err
	}

	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		return nil, nil
	}

	return strings.Split(trimmed, "\n"), nil
}

func nullFields(out string) []string {
	if out == "" {
		return nil
	}

	fields := strings.Split(out, "\x00")
	if fields[len(fields)-1] == "" {
		fields = fields[:len(fields)-1]
	}
	return fields
}

func gitSuccess(repoDir string, args ...string) bool {
	cmd := exec.Command("git", append([]string{"-C", repoDir}, args...)...)
	return cmd.Run() == nil
}

func gitOutput(repoDir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", repoDir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}

	return string(out), nil
}

func remoteBranchName(ref string) string {
	if i := strings.IndexByte(ref, '/'); i >= 0 && i+1 < len(ref) {
		return ref[i+1:]
	}
	return ""
}
