package sseclient

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	ErrUnexpectedStatus      error // ErrUnexpectedStatus indicates HTTP status != 200.
	ErrUnexpectedContentType error // ErrUnexpectedContentType indicates non text/event-stream response.
)

const maxOpenErrorBodyBytes = 8 * 1024

func init() {
	ErrUnexpectedStatus = errors.New("unexpected status code")
	ErrUnexpectedContentType = errors.New("unexpected content type")
}

// Client opens SSE HTTP connections and decodes text/event-stream responses.
//
// Reconnect policy is caller-managed.
type Client struct {
	httpClient     *http.Client // The HTTP client sends stream requests.
	defaultHeaders http.Header  // Default headers are added to opened requests unless the request already sets the header.
}

// Option configures Client.
type Option func(*Client)

// New constructs a Client. Defaults: http.DefaultClient, no extra default headers.
func New(opts ...Option) *Client {
	c := &Client{
		httpClient:     http.DefaultClient,
		defaultHeaders: make(http.Header),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
	if c.httpClient == nil {
		c.httpClient = http.DefaultClient
	}
	return c
}

// WithHTTPClient sets client used for requests. Nil means http.DefaultClient.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		c.httpClient = hc
	}
}

// WithHeader adds a default header for opened requests. Request-specific header values win over defaults.
func WithHeader(key, value string) Option {
	return func(c *Client) {
		c.defaultHeaders.Add(key, value)
	}
}

// OpenError wraps failures from OpenRequest and OpenURL. Use errors.Is(err, ErrUnexpectedStatus/ErrUnexpectedContentType).
type OpenError struct {
	Request      *http.Request  // Request is the request that was attempted; it may be nil for setup failures.
	Response     *http.Response // nil for transport/setup failures
	ResponseBody []byte         // ResponseBody contains captured response bytes for status or content-type failures when available.
	Err          error          // Err is the underlying open failure.
}

// Error returns a human-readable description of the open failure.
//
// The message includes request information when available and adds response status or content type details for handshake validation failures.
func (e *OpenError) Error() string {
	if e == nil {
		return "<nil>"
	}
	errText := "<nil>"
	if e.Err != nil {
		errText = e.Err.Error()
	}
	if errors.Is(e.Err, ErrUnexpectedStatus) && e.Response != nil {
		errText = fmt.Sprintf("%s: %s", errText, e.Response.Status)
	}
	if errors.Is(e.Err, ErrUnexpectedContentType) && e.Response != nil {
		contentType := e.Response.Header.Get("Content-Type")
		if contentType != "" {
			errText = fmt.Sprintf("%s: %s", errText, contentType)
		}
	}
	if e.Request == nil {
		return fmt.Sprintf("open sse stream: %s", errText)
	}
	return fmt.Sprintf("open sse stream %s %s: %s", e.Request.Method, e.Request.URL.String(), errText)
}

// Unwrap returns the underlying open failure.
//
// It returns nil for a nil receiver.
func (e *OpenError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Stream decodes SSE events from one HTTP response body.
type Stream struct {
	response   *http.Response  // response is the successful handshake response being consumed.
	results    chan recvResult // results carries decoded events and receive errors from the read loop.
	mu         sync.RWMutex    // mu protects state and close-related flags shared with the read loop.
	state      State           // state tracks sticky SSE reconnect hints observed while parsing.
	userClosed bool            // userClosed records whether Close was called by the caller.
	closeErr   error           // closeErr records the result of closing the response body.
	closeOnce  sync.Once       // closeOnce ensures the response body is closed at most once.
}

// OpenRequest issues req and returns a stream on success.
//
// Behavior:
//   - req.Context controls request lifetime.
//   - If Accept is unset, sets Accept: text/event-stream.
//   - Fails with *OpenError on transport/handshake problems.
func (c *Client) OpenRequest(req *http.Request) (*Stream, error) {
	if req == nil {
		return nil, &OpenError{Err: errors.New("nil request")}
	}

	usedReq := req.Clone(req.Context())
	if usedReq.Header == nil {
		usedReq.Header = make(http.Header)
	}

	if c != nil {
		for key, values := range c.defaultHeaders {
			if len(usedReq.Header.Values(key)) > 0 {
				continue
			}
			for _, v := range values {
				usedReq.Header.Add(key, v)
			}
		}
	}
	if len(usedReq.Header.Values("Accept")) == 0 {
		usedReq.Header.Set("Accept", "text/event-stream")
	}

	hc := http.DefaultClient
	if c != nil && c.httpClient != nil {
		hc = c.httpClient
	}
	resp, err := hc.Do(usedReq)
	if err != nil {
		return nil, &OpenError{
			Request: usedReq,
			Err:     err,
		}
	}
	if resp.StatusCode != http.StatusOK {
		body, closeErr := readOpenErrorBody(resp)
		if closeErr != nil {
			body = nil
		}
		_ = resp.Body.Close()
		return nil, &OpenError{
			Request:      usedReq,
			Response:     resp,
			ResponseBody: body,
			Err:          ErrUnexpectedStatus,
		}
	}
	if !isEventStreamContentType(resp.Header.Get("Content-Type")) {
		body, closeErr := readOpenErrorBody(resp)
		if closeErr != nil {
			body = nil
		}
		_ = resp.Body.Close()
		return nil, &OpenError{
			Request:      usedReq,
			Response:     resp,
			ResponseBody: body,
			Err:          ErrUnexpectedContentType,
		}
	}

	s := &Stream{
		response: resp,
		results:  make(chan recvResult),
	}
	go s.readLoop()
	return s, nil
}

// OpenURL is a convenience for GET requests.
func (c *Client) OpenURL(ctx context.Context, url string) (*Stream, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, &OpenError{
			Err: err,
		}
	}
	return c.OpenRequest(req)
}

// Recv blocks until next dispatched event or end-of-stream. Returns io.EOF on clean stream close.
func (s *Stream) Recv() (Event, error) {
	return s.RecvContext(context.Background())
}

// RecvContext allows per-receive cancellation/deadline control.
func (s *Stream) RecvContext(ctx context.Context) (Event, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case res, ok := <-s.results:
		if !ok {
			return Event{}, io.EOF
		}
		return res.event, res.err
	case <-ctx.Done():
		return Event{}, ctx.Err()
	}
}

// Close closes response body. Idempotent.
func (s *Stream) Close() error {
	if s == nil || s.response == nil || s.response.Body == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.userClosed = true
		s.mu.Unlock()
		s.closeErr = s.response.Body.Close()
	})
	return s.closeErr
}

// Response returns handshake response metadata.
func (s *Stream) Response() *http.Response {
	if s == nil {
		return nil
	}
	return s.response
}

// State returns parser-managed reconnect state. LastEventID and Retry are sticky per SSE processing model.
func (s *Stream) State() State {
	if s == nil {
		return State{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

// State carries reconnect-relevant stream state.
type State struct {
	LastEventID string        // LastEventID is the sticky last event ID observed while parsing.
	Retry       time.Duration // zero means no server retry hint observed
}

// Event is one dispatched SSE event.
type Event struct {
	ID   string // ID is effective last-event-id at dispatch time.
	Type string // Type defaults to "message" when not specified.
	Data string // Data is concatenated data lines joined with "\n" (no trailing newline).
}

// recvResult carries one read-loop result to a stream receiver.
type recvResult struct {
	event Event // The decoded event is returned when err is nil.
	err   error // The receive error is returned instead of an event when non-nil.
}

// readLoop reads and dispatches SSE events from the response body.
//
// It updates sticky reconnect state while parsing, sends decoded events and terminal errors on results, treats caller-initiated Close as clean EOF, and closes results
// before returning.
func (s *Stream) readLoop() {
	reader := newLineReader(s.response.Body)
	var (
		dataBuf   strings.Builder
		eventType string
	)
	for {
		line, err := reader.ReadLine()
		if err != nil {
			if errors.Is(err, io.EOF) {
				// Per SSE processing model, pending data at EOF is discarded unless
				// a blank line triggered dispatch before EOF.
				s.results <- recvResult{err: io.EOF}
				close(s.results)
				return
			}
			if s.wasClosedByUser() {
				s.results <- recvResult{err: io.EOF}
			} else {
				s.results <- recvResult{err: err}
			}
			close(s.results)
			return
		}

		if line == "" {
			if ev, ok := s.dispatch(dataBuf.String(), eventType); ok {
				s.results <- recvResult{event: ev}
			}
			dataBuf.Reset()
			eventType = ""
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}

		field, value := splitField(line)
		switch field {
		case "data":
			dataBuf.WriteString(value)
			dataBuf.WriteByte('\n')
		case "event":
			eventType = value
		case "id":
			if !strings.ContainsRune(value, 0) {
				s.mu.Lock()
				s.state.LastEventID = value
				s.mu.Unlock()
			}
		case "retry":
			if d, ok := parseRetry(value); ok {
				s.mu.Lock()
				s.state.Retry = d
				s.mu.Unlock()
			}
		}
	}
}

// dispatch converts a completed SSE event buffer into an Event.
//
// It returns false when no data is pending. When dispatching, it defaults an empty event type to "message", trims the trailing data newline, and uses the current
// sticky LastEventID.
func (s *Stream) dispatch(data, eventType string) (Event, bool) {
	if data == "" {
		return Event{}, false
	}
	data = strings.TrimSuffix(data, "\n")
	if eventType == "" {
		eventType = "message"
	}

	s.mu.RLock()
	lastEventID := s.state.LastEventID
	s.mu.RUnlock()

	return Event{
		ID:   lastEventID,
		Type: eventType,
		Data: data,
	}, true
}

// wasClosedByUser reports whether the caller has closed the stream.
func (s *Stream) wasClosedByUser() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.userClosed
}

func splitField(line string) (string, string) {
	idx := strings.IndexByte(line, ':')
	if idx < 0 {
		return line, ""
	}
	value := strings.TrimPrefix(line[idx+1:], " ")
	return line[:idx], value
}

func parseRetry(value string) (time.Duration, bool) {
	if value == "" {
		return 0, false
	}
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return 0, false
		}
	}
	ms, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, false
	}
	if ms > int64((time.Duration(1<<63-1))/time.Millisecond) {
		return 0, false
	}
	return time.Duration(ms) * time.Millisecond, true
}

func isEventStreamContentType(contentType string) bool {
	if contentType == "" {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = contentType
		if idx := strings.IndexByte(mediaType, ';'); idx >= 0 {
			mediaType = mediaType[:idx]
		}
		mediaType = strings.TrimSpace(mediaType)
	}
	return strings.EqualFold(mediaType, "text/event-stream")
}

func readOpenErrorBody(resp *http.Response) ([]byte, error) {
	if resp == nil || resp.Body == nil {
		return nil, nil
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxOpenErrorBodyBytes))
}

// lineReader reads SSE wire-format lines from an input stream.
//
// It recognizes LF, CR, and CRLF line endings, converts invalid UTF-8 to U+FFFD, and strips a leading UTF-8 BOM from the first line.
type lineReader struct {
	r       *bufio.Reader // The buffered reader supplies bytes from the stream.
	atStart bool          // atStart reports whether the next completed line is the first line.
}

func newLineReader(r io.Reader) *lineReader {
	return &lineReader{
		r:       bufio.NewReader(r),
		atStart: true,
	}
}

// ReadLine reads the next SSE line without its line ending.
//
// It accepts LF, CR, and CRLF endings. If EOF occurs after line bytes have been read, ReadLine returns the final unterminated line instead of io.EOF.
func (l *lineReader) ReadLine() (string, error) {
	var b []byte
	for {
		ch, err := l.r.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) && len(b) > 0 {
				return l.finishLine(b), nil
			}
			return "", err
		}

		switch ch {
		case '\n':
			return l.finishLine(b), nil
		case '\r':
			if next, err := l.r.Peek(1); err == nil && len(next) == 1 && next[0] == '\n' {
				_, _ = l.r.ReadByte()
			}
			return l.finishLine(b), nil
		default:
			b = append(b, ch)
		}
	}
}

// finishLine converts raw line bytes into a decoded SSE line.
//
// It replaces invalid UTF-8 with U+FFFD and strips a leading UTF-8 BOM from only the first line.
func (l *lineReader) finishLine(b []byte) string {
	line := strings.ToValidUTF8(string(b), "\uFFFD")
	if l.atStart {
		l.atStart = false
		line = strings.TrimPrefix(line, "\uFEFF")
	}
	return line
}
