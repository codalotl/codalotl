# sseclient

sseclient implements a minimal SSE client to consume HTTP APIs that stream data back. Plays well with the stdlib `net/http`.

## Spec

Follows the SSE wire format parsing rules defined at https://html.spec.whatwg.org/multipage/server-sent-events.html
- Saved locally as: `./whatwg.org.sse_spec.html` (on 2026-02-25)
- Converted to markdown as: `./whatwg.org.sse_spec.md`

## Usage

Basic read loop:

```go
ctx := context.Background()

c := sseclient.New(
	sseclient.WithHTTPClient(http.DefaultClient),
	sseclient.WithHeader("Authorization", "Bearer "+token),
)

stream, err := c.OpenURL(ctx, streamURL)
if err != nil {
	return err
}
defer stream.Close()

resp := stream.Response()
_ = resp // optional: inspect headers, status, final URL, etc

for {
	ev, err := stream.Recv()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	handle(ev.Type, ev.ID, ev.Data)
}
```

Manual reconnect loop (Last-Event-ID):

```go
ctx := context.Background()

c := sseclient.New(sseclient.WithHTTPClient(http.DefaultClient))

var (
	lastEventID string
	retry       = 2 * time.Second
)
for {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
	if err != nil {
		return err
	}
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}

	stream, err := c.OpenRequest(req)
	if err != nil {
		var openErr *sseclient.OpenError
		if errors.As(err, &openErr) {
			// optional: use openErr.Response for status/header handling
		}
		time.Sleep(retry)
		continue
	}

	for {
		ev, err := stream.Recv()
		if err != nil {
			st := stream.State()
			if st.LastEventID != "" {
				lastEventID = st.LastEventID
			}
			if st.Retry > 0 {
				retry = st.Retry
			}
			_ = stream.Close()
			break // reconnect
		}
		lastEventID = ev.ID
		handle(ev.Type, ev.ID, ev.Data)
	}

	time.Sleep(retry)
}
```

## Dependencies

No third-party deps. Only stdlib.

## Public API

```go
import (
	"context"
	"net/http"
	"time"
)

// Client opens SSE HTTP connections and decodes text/event-stream responses.
//
// Reconnect policy is caller-managed.
type Client struct{}

// Option configures Client.
type Option func(*Client)

// New constructs a Client. Defaults: http.DefaultClient, no extra default headers.
func New(opts ...Option) *Client

// WithHTTPClient sets client used for requests. Nil means http.DefaultClient.
func WithHTTPClient(hc *http.Client) Option

// WithHeader adds a default header for opened requests. Request-specific header values win over defaults.
func WithHeader(key, value string) Option

var (
	ErrUnexpectedStatus      error // ErrUnexpectedStatus indicates HTTP status != 200.
	ErrUnexpectedContentType error // ErrUnexpectedContentType indicates non text/event-stream response.
)

// OpenError wraps failures from OpenRequest and OpenURL. Use errors.Is(err, ErrUnexpectedStatus/ErrUnexpectedContentType).
type OpenError struct {
	Request  *http.Request
	Response *http.Response // nil for transport/setup failures
	Err      error
}

func (e *OpenError) Error() string
func (e *OpenError) Unwrap() error

// Stream decodes SSE events from one HTTP response body.
type Stream struct{}

// OpenRequest issues req and returns a stream on success.
//
// Behavior:
//   - req.Context controls request lifetime.
//   - If Accept is unset, sets Accept: text/event-stream.
//   - Fails with *OpenError on transport/handshake problems.
func (c *Client) OpenRequest(req *http.Request) (*Stream, error)

// OpenURL is a convenience for GET requests.
func (c *Client) OpenURL(ctx context.Context, url string) (*Stream, error)

// Recv blocks until next dispatched event or end-of-stream. Returns io.EOF on clean stream close.
func (s *Stream) Recv() (Event, error)

// RecvContext allows per-receive cancellation/deadline control.
func (s *Stream) RecvContext(ctx context.Context) (Event, error)

// Close closes response body. Idempotent.
func (s *Stream) Close() error

// Response returns handshake response metadata.
func (s *Stream) Response() *http.Response

// State returns parser-managed reconnect state. LastEventID and Retry are sticky per SSE processing model.
func (s *Stream) State() State

// State carries reconnect-relevant stream state.
type State struct {
	LastEventID string
	Retry       time.Duration // zero means no server retry hint observed
}

// Event is one dispatched SSE event.
type Event struct {
	ID   string // ID is effective last-event-id at dispatch time.
	Type string // Type defaults to "message" when not specified.
	Data string // Data is concatenated data lines joined with "\n" (no trailing newline).
}
```
