// Package tui implements the interactive terminal UI for the coding agent.
//
// The UI runs in an alternate screen and provides a chat-style messages area, text input, progress indication, permission prompts, mouse scrolling, and overlay
// actions such as copying displayed messages. Users can send messages to the agent, interrupt running work, and use slash commands to start new sessions, select
// models, enter package mode, or start orchestrator sessions.
//
// Use Run to start the UI with default settings, or RunWithConfig to customize runtime behavior such as the palette, model, lint steps, permission handling, CAS-backed
// metadata checks, model persistence, and remote monitoring.
package tui
