// Package llmstream provides a unified streaming interface for LLM providers.
//
// Use NewConversation to build a mutable conversation, add user turns, tools, and tool results, and call SendAsync to receive provider events until a final assistant
// turn is appended. Use NewCompleter for one-shot text completions.
//
// The package models conversation state as Turns containing text, reasoning, tool calls, tool results, and provider state needed for safe replay. It also defines
// common tool metadata and semantic presentation types for consumers that render tool activity.
package llmstream
