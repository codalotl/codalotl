// Package anthropic provides a small streaming client for Anthropic's Messages API.
//
// Create a Client with New, then call Client.StreamMessages to start a streaming POST /v1/messages request. The returned Stream yields decoded Anthropic events
// with Recv or RecvContext until message_stop, after which Recv returns io.EOF. Close the stream when abandoning a request before it completes.
//
// The package implements only the streaming Messages API, not the full Anthropic API surface. It sends Anthropic's default API version by default and always enables
// the context-1m-2025-08-07 beta feature; use Options to override the base URL, HTTP client, API version, or to append additional beta features.
package anthropic
