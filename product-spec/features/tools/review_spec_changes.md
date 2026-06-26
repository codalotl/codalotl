# `review_spec_changes`

`review_spec_changes` gets focused feedback on recent `SPEC.md` edits for one Go package.

LLMs by default tend to over-specify in `SPEC.md` files. They're too verbose. They don't think about the end-user. They get lost in irrelevant details. By giving feedback on these changes, we can force better behavior.

## Inputs

- `package`: required string. A Go package directory, current-module relative package path, or Go import path.
- `message`: required string. Context for the review request, including what changed, why it changed, whether the changes are uncommitted or otherwise located, and any implementation details that should inform review without necessarily belonging in `SPEC.md`.

## Output

The tool returns the delegated agent's plain-text feedback.

Errors include invalid parameters, package-resolution failures, subagent startup failures, and delegated review failures.

Example output:

```text
- Do you understand the changes to SPEC.md and the user's context? Are the changes logically coherent with the rest of the SPEC?

Yes. The SPEC edit adds one provider behavior and fits coherently with the surrounding streaming bullets.

- Are the changes implementable? Do you have any concerns about implementing them in this package?

Yes, implementable. This package already owns the relevant streaming and retry behavior.

- Is there significant ambiguity where you don't know of any reasonable solution?

No. There is ambiguity in exact error classification, but a reasonable implementation can classify common transport disconnects while leaving provider semantic failures non-retryable.
```

## Behavior

- The orchestrator supplies one package selector and a message with review context.
- The package selector may be a Go package directory, a current-module relative package path, or a Go import path.
- The tool launches a limited package-mode review agent for that package.
- The review agent receives package-mode context and the orchestrator's message.
- The review agent uses the `$spec-md` guidance to review the latest `SPEC.md` changes in that package.
- The changes under review may be uncommitted edits, a newly created `SPEC.md`, or recent committed edits identified from git history and the orchestrator's message.
- Review focuses on whether the `SPEC.md` edits are coherent, implementable, correctly located, appropriately terse, timeless, and at the right level of detail.
- The review agent should not edit files or implement the planned change.
- The review agent should not complain that package code does not yet implement the edited `SPEC.md`; these reviews usually happen before implementation.
- The tool is advisory. The orchestrator decides whether to revise the spec and when the spec is good enough.

## Presentation

Example display:

```text
• Reviewing SPEC changes in internal/foo
  └ Background: See @.prs/example.md for context.
  • [...]
  • [... subagent working ...]
  • [...]
• Reviewed SPEC changes in internal/foo
  └ Do you understand the changes to SPEC.md and the user's context? Yes...
```
