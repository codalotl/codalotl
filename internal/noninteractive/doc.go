// Package noninteractive runs agents without an interactive TUI.
//
// It provides Exec for one-shot CLI-style runs and NewSession for reusable conversations that preserve agent state across user messages. Output is written as human-readable
// text by default, or as newline-delimited JSON when Options.OutputJSON is set.
package noninteractive
