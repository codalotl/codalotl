# `review`

`review` delegates a code review of the current branch against a base branch or ref.

It is for validating the committed implementation state of a PR-style branch, not for reviewing package `SPEC.md` edits or making changes directly.

## Inputs

- `base`: required branch or ref name to review against, often `main`, `master`, or a remote-tracking ref like `origin/main`.

## Output

On success, the tool returns normalized JSON with:

- `findings`: review findings, each with a priority-tagged title, markdown body, confidence score, optional numeric priority, and a code location using an absolute file path plus a short line range.
- `overall_correctness`: a verdict of either `patch is correct` or `patch is incorrect`.
- `overall_explanation`: a short explanation for the verdict.
- `overall_confidence_score`: confidence in the overall verdict.

An empty findings list is valid when the review found no actionable bugs.

Errors include invalid parameters, an unavailable or invalid base ref, git command failures while collecting review context, subagent failures, and review output that cannot be returned as the expected JSON result.

## Behavior

- The orchestrator supplies the base branch or ref to review against.
- The tool launches a dedicated review subagent.
- The review subagent receives enough git context to understand the branch, including commit log, diff stat, and full diff from the base ref to `HEAD`.
- The review subagent may inspect the repository as needed.
- The review subagent does not edit files or commit.
- Review findings are limited to actionable bugs introduced by the reviewed diff.
- The review should not report ordinary style comments, pre-existing issues, or speculative risks without a concrete affected path.
- The review result is advisory. The orchestrator decides whether each finding should be fixed, rejected, deferred, or treated as out of scope.

## Presentation

Example display while running:

```text
• Reviewing origin/main
```

Example display with findings:

```text
• Reviewed origin/main
  └ [P1] Preserve machine-readable review output
    [P2] Avoid stale diff context after follow-up commits
```

When the review completes without findings:

```text
• Reviewed origin/main
  └ No actionable findings.
```

The presentation should show concise finding titles rather than dumping the full diff, commit log, or raw JSON into the human transcript. Large finding sets may be summarized after the first several titles.

## Permissions

The tool reads git history and repository contents needed for review. It does not write files, apply patches, or commit.

Repository reads remain subject to the ordinary sandbox and authorization rules of the orchestrator and the review subagent.
