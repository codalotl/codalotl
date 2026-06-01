// Package sseclient implements a minimal Server-Sent Events (SSE) client for consuming HTTP text/event-stream responses.
//
// It parses SSE frames according to the WHATWG wire format, exposes dispatched events, and preserves reconnect hints such as the last event ID and retry delay so
// callers can manage their own reconnect policy. It works with the standard net/http package and does not implement full browser EventSource behavior such as automatic
// reconnects, readyState, or DOM event dispatch.
package sseclient
