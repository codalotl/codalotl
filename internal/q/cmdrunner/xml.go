package cmdrunner

import (
	"fmt"
	"strings"
	"time"
)

// DurationWarnThreshold controls when a command duration is surfaced in ToXML output.
const DurationWarnThreshold = 5 * time.Second

// ToXML renders command results in a lightweight XML-like format intended for LLM consumption.
func (r Result) ToXML(tag string) string {
	if tag == "" {
		tag = "result"
	}

	switch len(r.Results) {
	case 0:
		okValue := boolString(r.Success())
		return fmt.Sprintf("<%s ok=\"%s\"></%s>", tag, okValue, tag)
	case 1:
		return renderResultBlock(tag, r.Results[0], false)
	default:
		var b strings.Builder
		b.WriteString("<")
		b.WriteString(tag)
		b.WriteString(` ok="`)
		b.WriteString(boolString(r.Success()))
		b.WriteString(`">`)
		b.WriteByte('\n')

		for _, res := range r.Results {
			b.WriteString(renderResultBlock("command", res, true))
		}

		b.WriteString("</")
		b.WriteString(tag)
		b.WriteString(">")
		return b.String()
	}
}

func renderResultBlock(tag string, res CommandResult, trailingNewline bool) string {
	var b strings.Builder

	b.WriteString("<")
	b.WriteString(tag)
	b.WriteString(commandAttributes(res))
	b.WriteString(">\n")
	b.WriteString("$ ")
	b.WriteString(formatCommandLine(res))
	b.WriteByte('\n')

	if res.Output != "" {
		b.WriteString(res.Output)
		if !strings.HasSuffix(res.Output, "\n") {
			b.WriteByte('\n')
		}
	}

	b.WriteString("</")
	b.WriteString(tag)
	b.WriteString(">")

	if trailingNewline {
		b.WriteByte('\n')
	}

	return b.String()
}

func commandAttributes(res CommandResult) string {
	parts := []string{fmt.Sprintf(`ok="%s"`, boolString(res.Outcome == OutcomeSuccess))}

	if res.ShowCWD && res.CWD != "" {
		parts = append(parts, fmt.Sprintf(`cwd="%s"`, res.CWD))
	}

	if res.MessageIfNoOutput != "" && res.Output == "" {
		parts = append(parts, fmt.Sprintf(`message="%s"`, res.MessageIfNoOutput))
	}

	if res.ExecStatus != "" && res.ExecStatus != ExecStatusCompleted {
		parts = append(parts, fmt.Sprintf(`exec-status="%s"`, res.ExecStatus))
	}
	if res.ExitCode != 0 && res.ExitCode != 1 {
		parts = append(parts, fmt.Sprintf(`exit-code="%d"`, res.ExitCode))
	}
	if res.Signal != "" {
		parts = append(parts, fmt.Sprintf(`signal="%s"`, res.Signal))
	}
	if res.Duration > DurationWarnThreshold {
		parts = append(parts, fmt.Sprintf(`duration="%s"`, formatWarnDuration(res.Duration)))
	}

	return " " + strings.Join(parts, " ")
}

func formatCommandLine(res CommandResult) string {
	if len(res.Args) == 0 {
		return res.Command
	}
	return res.Command + " " + strings.Join(res.Args, " ")
}

func formatWarnDuration(d time.Duration) string {
	return fmt.Sprintf("%.1fs", d.Seconds())
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
