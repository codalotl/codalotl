# PR

## User Summary (do not modify)

In internal/agent:

Add a Parent string field to AgentMeta:

```go
type AgentMeta struct {
	ID    string // stable, unique per Agent/SubAgent within a session
	Depth int    // 0 == root agent
    Parent string // parent Agent/SubAgent ID ("" if this is the root agent)
}
```

Don't use this anywhere else. Update the integration tests if necessary.

## Plan

### [DONE] `internal/agent`

- Add `Parent string` to `agent.AgentMeta`.
- Root agent events should report `Parent == ""`.
- SubAgent events should report the parent agent's ID.
- Keep the change local to event metadata; no new behavior should consume `Parent`.
- Update `internal/agent/SPEC.md` to match the public event shape.

### [DONE] Tests and fixtures

- Add or update focused `internal/agent` tests covering root and nested agent metadata.
- Update integration fixtures only if an existing serialized event shape now includes `Parent`.
  - Verified with `go test ./internal/agent ./internal/noninteractive/...`.
  - No integration fixture updates were needed.

## Review

- `codex review --base main` found no actionable correctness issues in the diff.
- Review-side `go test ./internal/agent/...` passed.
- Review-side `go test ./internal/noninteractive/...` hit local environment failures unrelated to this change:
  - skill installer test could not remove a file under `/Users/jonathannovak/.codalotl/skills/.system/...` due to permissions
  - integration test `TestAugmentReplayMockOpenAIErrorIncludesPrunedActualAndExpectedRequests` could not bind an `httptest` listener in this environment

## Summary

- Added `Parent string` to `agent.AgentMeta` and populated it from the immediate parent agent in event metadata.
- Root agent events report `Parent == ""`; child and grandchild events report the immediate parent agent ID.
- Added focused `internal/agent` coverage for root events, mirrored child events, and nested subagent metadata propagation.
