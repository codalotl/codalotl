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
