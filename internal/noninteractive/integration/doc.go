// Package integration records and replays end-to-end noninteractive JSON-mode test cases against a mock OpenAI server.
//
// The package is intended for regression tests that exercise the agent, JSON event stream, tool execution, linting, repository access controls, and mock transport
// together. Cases are stored as directories containing recorded configuration, mock provider traffic, and optional expected repository snapshots.
package integration
