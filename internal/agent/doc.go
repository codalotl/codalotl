// Package agent wraps llmstream conversations in a tool-aware event loop.
//
// An Agent owns conversation state, appends user and tool-result turns, sends the conversation to the configured model, and emits structured Event values that describe
// progress. Callers typically construct an agent with New, optionally seed additional context with AddUserTurn, then call SendUserMessage and drain the returned
// channel until it closes.
//
// The event stream is designed for interactive consumers such as the TUI. It includes presentation-oriented assistant text and reasoning events, tool call and tool
// completion events, completed assistant turns, queued-message notifications, and exactly one terminal event reporting success, cancellation, or failure. QueueUserMessage
// lets callers extend an active run by appending more user input at safe boundaries without starting a second run loop.
//
// Tools can create subagents while servicing a tool call. A tool retrieves a SubAgentCreator from the tool context with SubAgentCreatorFromContext, creates a child
// Agent, and runs it with its own prompt and tool set. Subagent events are mirrored onto the parent stream and annotated with AgentMeta and StartSubagent so consumers
// can attribute them to the correct agent within the shared session.
//
// For callers that only need the final answer text from a run, CollectFinalAssistantText drains an event stream and returns the root agent's final assistant text.
package agent
