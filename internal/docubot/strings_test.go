package docubot

import "github.com/codalotl/codalotl/internal/gocodetesting"

var dedent = gocodetesting.Dedent

// wrapWithBackticks wraps a string with Go code block backticks.
func wrapWithBackticks(s string) string {
	return "```go\n" + s + "\n```"
}

func dedentWithBackticks(s string) string {
	return wrapWithBackticks(dedent(s))
}
