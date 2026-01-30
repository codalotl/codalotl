package health

type HumanErr struct {
	HumanMessage string
	HealthErr
}

// NewHumanErr returns a HumanErr, which has both a message suitable for end-users and a message suitable for logging.
func NewHumanErr(humanMsg string, msg string, args ...any) error {
	return &HumanErr{HumanMessage: humanMsg, HealthErr: HealthErr{Message: msg, attrs: args}}
}

// Error satisfies the error interface.
//
// Only the human message will appear here (unless its empty). The logging-suitable message can be accessed via e.HealthErr.Error().
func (e *HumanErr) Error() string {
	return e.HumanMessage
}
