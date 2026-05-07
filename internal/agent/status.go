package agent

// Status indicates whether the agent is processing a turn.
type Status int

// Status values describe an Agent's run state.
const (
	StatusIdle    Status = iota // StatusIdle means the agent is not processing a run.
	StatusRunning               // StatusRunning means the agent is processing a run.
)
