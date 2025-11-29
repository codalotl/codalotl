package agent

// Status indicates whether the agent is processing a turn.
type Status int

const (
	StatusIdle Status = iota
	StatusRunning
)
