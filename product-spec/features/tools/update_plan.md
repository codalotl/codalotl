# `update_plan`

`update_plan` shows the user the agent's current working checklist. It helps communicate to the user what the agent's plan is, and is also a forcing function for the agent to actually have a plan. It does not inspect the project, run commands, or edit files.

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
- At most one should be `in_progress`.
- Zero or more `completed` items are first; then the optional `in_progress` item; then then zero or more pending items.
- The agent should call `update_plan` multiple times over the course of a session to keep track of its work.
- Besides being shown to the user, nothing is done with this data.

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
