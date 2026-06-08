# `review`

`review` lets the PR Orchestrator delegate a full code review of the current branch against a base branch or ref.

It is for validating the committed implementation state of a PR-style branch, not for reviewing package `SPEC.md` edits or making changes directly.

## Availability

- Available to the PR Orchestrator.
- Not available as a general-purpose package-mode tool.
- Used after implementation work is far enough along to review the branch against its intended base.

## Behavior

- The orchestrator supplies the base branch or ref to review against.
- The tool launches a dedicated review subagent.
- The review subagent receives enough git context to understand the branch:
    - commit log from the base ref to `HEAD`;
    - diff stat against the base ref;
    - full diff against the base ref, including rename-aware diff output and submodule diff details when present.
- The review subagent may inspect the repository as needed, but it does not edit files or commit.
- Review findings are limited to actionable bugs introduced by the reviewed diff. The review should not report ordinary style comments, pre-existing issues, or speculative risks without a concrete affected path.
- The review result is advisory. The orchestrator sanity-checks it, records it in the PR file, and decides whether each finding should be fixed, rejected, deferred, or treated as out of scope.

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

## Presentation

Human-facing output presents review as a long-running delegated operation.

When the review starts:

```text
• Reviewing origin/main
```

When the review completes with findings:

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

The review subagent's final answer should not appear as a separate duplicate message; the tool result is the visible review completion.

## Permissions

The tool reads git history and repository contents needed for review. It does not write files, apply patches, or commit.

Repository reads remain subject to the ordinary sandbox and authorization rules of the orchestrator and the review subagent.
