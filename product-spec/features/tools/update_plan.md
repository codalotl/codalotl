# `update_plan`

`update_plan` shows the user the agent's current working checklist.

It is for visible task planning and progress reporting. It does not inspect the project, run commands, or edit files.

## Inputs

- `explanation`: optional text shown above the checklist when non-empty.
- `plan`: required ordered list of plan items.
- `plan[].step`: short user-facing description of the work item.
- `plan[].status`: `pending`, `in_progress`, or `completed`.

## Output

On success, the tool accepts the plan update and returns a short confirmation for the agent.

Errors include malformed parameters, a missing `plan` field, blank steps, blank statuses, unsupported statuses, and more than one `in_progress` item.

Example output:

```text
Plan updated
```

## Behavior

- The agent supplies an ordered list of plan items.
- Each plan item has a short user-facing step and a status.
- Status is one of `pending`, `in_progress`, or `completed`.
- At most one item may be marked `in_progress`.
- The plan may be empty when the agent only needs to show an overview.
- A new call replaces the previous visible plan presentation for that agent.
- The checklist preserves item order so the user can see what was done, what is active, and what remains.
- The first unfinished item is treated as the next-up item. Explicit `in_progress` items are also emphasized.

## Presentation

Example display:

```text
• Update Plan
  └ Inspect existing product specs
    [x] Read summary and related docs
    [ ] Draft formatting feature spec
    [ ] Verify style and save file
```

Completed items use checked checklist marks. Pending and in-progress items use unchecked checklist marks, with emphasis indicating the current or next-up work.

The presentation should keep the checklist compact and scannable.

## Permissions

`update_plan` does not read or write the filesystem and does not require filesystem authorization.
