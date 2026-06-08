# `review_spec_changes`

`review_spec_changes` lets the PR orchestrator get focused feedback on recent `SPEC.md` edits for one Go package before implementation starts.

## Availability

- Available to the PR orchestrator.
- Intended for planning and design steps where the orchestrator is editing package `SPEC.md` files.
- Delegates to a limited package-mode agent for the selected package.

## Behavior

- The orchestrator supplies one package selector and a message with review context.
- The package selector resolves like other package-mode package inputs: it may be a Go package directory, a current-module relative package path, or a Go import path.
- The tool launches a `limited_package_mode` subagent for that package.
- The subagent receives package-mode context and the orchestrator's message.
- The subagent uses the `$spec-md` guidance to review the latest `SPEC.md` changes in that package.
- The changes under review may be uncommitted edits, a newly created `SPEC.md`, or recent committed edits identified from git history and the orchestrator's message.
- Review focuses on whether the `SPEC.md` edits are coherent, implementable, correctly located, appropriately terse, timeless, and at the right level of detail.
- Review considers the edited `SPEC.md` together with the orchestrator's message. The message can contain motivation, PR-file references, and details that are intentionally outside the `SPEC.md`.
- The tool is advisory. The orchestrator remains responsible for judging the feedback, revising the spec when useful, and deciding when the spec is good enough.
- The delegated review agent should not edit files and should not implement the planned change.
- The delegated review agent should not complain that package code does not yet implement the edited `SPEC.md`; these reviews usually happen before implementation.

## Inputs

- `package`: required string. A Go package directory, current-module relative package path, or Go import path.
- `message`: required string. Context for the review request, including what changed, why it changed, whether the changes are uncommitted or otherwise located, and any implementation details that should inform review without necessarily belonging in `SPEC.md`.

## Output

The tool returns the delegated agent's plain-text feedback.

The feedback should answer the configured review questions at a product level rather than returning raw tool data. It may identify concerns, suggest concise `SPEC.md` edits, or say that the edits are coherent enough to proceed.

Errors include invalid parameters, package-resolution failures, subagent startup failures, and delegated review failures.

## Presentation

Human-facing output uses an append-style subagent Q-and-A presentation.

In progress:

```text
• Reviewing SPEC changes in internal/foo
  └ Background: See @.prs/example.md for context.
```

Completion:

```text
• Reviewed SPEC changes in internal/foo
  └ Do you understand the changes to SPEC.md and the user's context? Yes...
```

Nested subagent activity may be shown between the in-progress and completion lines. The delegated agent's final assistant message is surfaced as the tool result body rather than repeated as a separate chat message.

## Permissions

The tool resolves the target package before launching the delegated package-mode review.

The delegated agent receives limited package-mode access for the selected package. Its intended work is read-only review of `SPEC.md` changes and supporting package context, even though the limited package-mode toolset may include ordinary package tools needed for package-aware investigation.
