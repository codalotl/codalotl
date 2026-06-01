// Package agentregistry defines and resolves named agents.
//
// A Registry stores agent definitions and tools by stable names. A definition describes the prompt, tools, initial turns, and authorization policy used to create
// an agent. Tools can also be selected dynamically from invocation options, allowing callers to vary the toolset without changing the definition.
//
// Call Prepare when a session-style caller needs the resolved configuration without starting a run, and Create when it needs an idle *agent.Agent with registry-provided
// initial turns already applied. Call Invoke for immediate execution of a named agent, including subagent-style calls from tools.
package agentregistry
