// Package prompt builds system prompts for LLM coding agents.
//
// Prompts are rendered from the configured agent name and model, and may vary by provider or available file-editing tools. The package provides a basic prompt for
// generic agents and Go package-mode prompts for agents that must operate within a single package, including an update-usage variant for subagents with reduced
// capabilities.
//
// Call SetAgentName and SetModel during startup to configure the defaults used by the prompt rendering functions.
package prompt
