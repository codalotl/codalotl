package tui

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"unicode"
)

// escapeForLog returns s with non-printable runes escaped so logs remain readable.
func escapeForLog(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsPrint(r) || r == '\n' || r == '\r' || r == '\t' {
			b.WriteRune(r)
		} else {
			if r < 0x100 {
				b.WriteString(fmt.Sprintf("\\x%02x", r))
			} else {
				b.WriteString(fmt.Sprintf("\\u%04x", r))
			}
		}
	}
	return b.String()
}

func debugLogf(format string, args ...interface{}) {
	if logFile := os.Getenv("TUI_LOG_FILE"); logFile != "" {
		f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			fmt.Fprintf(os.Stderr, "debugLogf failed to open %s: %v\n", logFile, err)
			return
		}
		defer f.Close()
		msg := fmt.Sprintf(format, args...)
		escaped := escapeForLog(msg)
		fmt.Fprint(f, escaped)
		fmt.Fprintln(f)
	}
}

func stripAnsi(s string) string {
	re := regexp.MustCompile(`\x1B\[[0-9;]*[mK]`)
	return re.ReplaceAllString(s, "")
}
