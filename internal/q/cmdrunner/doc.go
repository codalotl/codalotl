// Package cmdrunner runs templated shell commands and reports structured results.
//
// A Runner validates inputs, renders Command templates with path helpers such as manifestDir, relativeTo, and repoDir, and executes commands in registration order.
// Run returns errors for invalid inputs or template failures; command execution failures are captured in the returned Result.
//
// Results expose command output, execution status, semantic outcome, exit code, signal, and duration, and can be rendered in a lightweight XML-like format for LLM
// and tool consumption.
package cmdrunner
