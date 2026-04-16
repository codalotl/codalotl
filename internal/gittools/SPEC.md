# gittools

The `gittools` package provides convenient APIs for working with git repos. It primarily shells out to `git`. It may also use `gh` as an optional hint.

## Merge Base

Primary use-case:
- identify commits and Go packages touched by current line of work
- especially when a user wants branch-authored changes rather than all current diffs vs target branch

This package offers a function to obtain a heuristic base:
- `HeuristicMergeBase` returns (base commit sha, optional base ref name, error)
    - Accept `repoDir`, any path inside a git working tree. Use `""` for cwd.
- Purpose:
    - return a best-effort commit/ref that separates commits on current line of work from upstream commits
    - intended to support building a commit range for "commits on this branch"
- This function is heuristic. Git cannot always recover the true historical source branch or exact original fork point.
    - Prefer a plausible answer rather than fail completely.
    - May use `gh` if available, but must also have non-`gh` heuristics.
- The ref name is optional.
    - Canonicalize equivalent local/remote refs before judging ambiguity.
    - Treat refs for current branch as self refs, not candidate base refs.
    - Return non-empty only when heuristics identify a single plausible logical base ref.
    - Prefer local branch names when an equivalent local branch exists.
    - If only a remote-tracking ref is known, may return that ref name.
- Ref ambiguity alone should not cause failure.
    - If ref name is ambiguous but a plausible base commit can still be identified, return the commit and `""` for ref.
- If an open GitHub PR exists for current branch, PR base branch is a strong hint for base ref selection.
- Returned commit need not be the literal historical commit where the branch was first created.
    - It should be a useful boundary for isolating commits that belong to current line of work.

User scenarios:
- Simple feature branch off `main`:
    - user creates `users-feature-branch` from `main`
    - co-workers continue merging into `main`
    - user fetches or pulls those updates
    - returns a base commit on `main` and ref name `main`
- Subbranch off another feature branch:
    - user creates `users-feature-branch-subbranch` from `users-feature-branch`
    - if heuristics can identify parent branch, return a base commit on that branch and `users-feature-branch`
- Rebase:
    - if user rebases branch onto newer `main`, returned base commit should move forward accordingly
    - goal is still to isolate commits that belong to rebased branch
- Merge from base branch:
    - if user merges newer `main` into feature branch, returned base commit should still permit excluding upstream commits from `main` while retaining commits unique to feature branch
- Ambiguous history:
    - if ref name cannot be inferred confidently, it may be `""`
    - function should still try to return a plausible base commit

## Changed Paths

This package also offers a function to identify changed repo paths relative to a base commit:
- `ChangedPathsSince` returns repo-relative changed paths since `baseCommit`
    - Accept `repoDir`, any path inside a git working tree. Use `""` for cwd.
    - Accept `baseCommit`, typically the commit returned by `HeuristicMergeBase`
    - Accept `includeUncommitted`
        - when false, return paths changed by committed work between `baseCommit` and `HEAD`
        - when true, also include staged, unstaged, and untracked working tree changes
    - Return sorted unique repo-relative paths
    - Include deleted paths
    - For renames and copies, include both old and new paths

Primary use-case:
- determine which files or directories the current line of work touches
- support higher-level tooling that maps changed paths to changed Go packages

User scenarios:
- Branch-authored commits only:
    - user wants paths changed by committed work on current branch
    - call `ChangedPathsSince(repoDir, baseCommit, false)`
- Include current checkout state:
    - user also wants staged, unstaged, or untracked edits considered
    - call `ChangedPathsSince(repoDir, baseCommit, true)`

## Public API

```go
// HeuristicMergeBase returns a best-effort base commit/ref for isolating commits on the current line of work.
func HeuristicMergeBase(repoDir string) (commit string, ref string, err error)

// ChangedPathsSince returns sorted unique repo-relative paths changed since baseCommit.
func ChangedPathsSince(repoDir string, baseCommit string, includeUncommitted bool) ([]string, error)
```
