package sseclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenRequest_AppliesDefaultsAndAcceptHeader(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "request-token", r.Header.Get("Authorization"))
		assert.Equal(t, "1", r.Header.Get("X-Default"))
		assert.Equal(t, "text/event-stream", r.Header.Get("Accept"))

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: ok\n\n")
	}))
	t.Cleanup(srv.Close)

	c := New(
		WithHTTPClient(srv.Client()),
		WithHeader("Authorization", "default-token"),
		WithHeader("X-Default", "1"),
	)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "request-token")

	stream, err := c.OpenRequest(req)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = stream.Close()
	})

	assert.NotNil(t, stream.Response())

	ev, err := stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, Event{
		ID:   "",
		Type: "message",
		Data: "ok",
	}, ev)

	_, err = stream.Recv()
	assert.ErrorIs(t, err, io.EOF)
}

func TestOpenRequest_UnexpectedStatus(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)

	c := New(WithHTTPClient(srv.Client()))
	stream, err := c.OpenURL(context.Background(), srv.URL)
	require.Error(t, err)
	assert.Nil(t, stream)
	assert.ErrorIs(t, err, ErrUnexpectedStatus)

	var openErr *OpenError
	require.True(t, errors.As(err, &openErr))
	assert.NotNil(t, openErr.Response)
	assert.Equal(t, http.StatusBadGateway, openErr.Response.StatusCode)
}

func TestOpenRequest_UnexpectedContentType(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	t.Cleanup(srv.Close)

	c := New(WithHTTPClient(srv.Client()))
	stream, err := c.OpenURL(context.Background(), srv.URL)
	require.Error(t, err)
	assert.Nil(t, stream)
	assert.ErrorIs(t, err, ErrUnexpectedContentType)

	var openErr *OpenError
	require.True(t, errors.As(err, &openErr))
	assert.NotNil(t, openErr.Response)
}

func TestStream_ParseAndState(t *testing.T) {
	t.Parallel()

	body := "\uFEFF: ignore comment\r\n" +
		"id: 1\r\n" +
		"event: update\r\n" +
		"data: first\r\n" +
		"data: second\r\n" +
		"retry: 1500\r\n" +
		"\r\n" +
		"retry: nope\n" +
		"data: lone\n" +
		"\n" +
		"id\n" +
		"data: reset\n" +
		"\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)

	c := New(WithHTTPClient(srv.Client()))
	stream, err := c.OpenURL(context.Background(), srv.URL)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = stream.Close()
	})

	ev, err := stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, Event{
		ID:   "1",
		Type: "update",
		Data: "first\nsecond",
	}, ev)
	assert.Equal(t, State{
		LastEventID: "1",
		Retry:       1500 * time.Millisecond,
	}, stream.State())

	ev, err = stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, Event{
		ID:   "1",
		Type: "message",
		Data: "lone",
	}, ev)
	assert.Equal(t, State{
		LastEventID: "1",
		Retry:       1500 * time.Millisecond,
	}, stream.State())

	ev, err = stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, Event{
		ID:   "",
		Type: "message",
		Data: "reset",
	}, ev)
	assert.Equal(t, State{
		LastEventID: "",
		Retry:       1500 * time.Millisecond,
	}, stream.State())

	_, err = stream.Recv()
	assert.ErrorIs(t, err, io.EOF)
}

func TestStream_RecvContextCancellationAndClose(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	c := New(WithHTTPClient(srv.Client()))
	stream, err := c.OpenURL(context.Background(), srv.URL)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	t.Cleanup(cancel)

	_, err = stream.RecvContext(ctx)
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	assert.NoError(t, stream.Close())
	assert.NoError(t, stream.Close())

	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)

	_, err = stream.RecvContext(ctx)
	assert.ErrorIs(t, err, io.EOF)
}

func TestStream_DiscardPendingDataAtEOF(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: incomplete\n")
	}))
	t.Cleanup(srv.Close)

	c := New(WithHTTPClient(srv.Client()))
	stream, err := c.OpenURL(context.Background(), srv.URL)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = stream.Close()
	})

	_, err = stream.Recv()
	assert.ErrorIs(t, err, io.EOF)
}

func TestStream_StripsOnlyLeadingBOM(t *testing.T) {
	t.Parallel()

	body := "data: first\n\n" +
		"\uFEFFdata: second\n\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)

	c := New(WithHTTPClient(srv.Client()))
	stream, err := c.OpenURL(context.Background(), srv.URL)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = stream.Close()
	})

	ev, err := stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, Event{
		ID:   "",
		Type: "message",
		Data: "first",
	}, ev)

	_, err = stream.Recv()
	assert.ErrorIs(t, err, io.EOF)
}

func TestStream_DataFieldWithoutColonDispatchesEmptyData(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data\n\n")
	}))
	t.Cleanup(srv.Close)

	c := New(WithHTTPClient(srv.Client()))
	stream, err := c.OpenURL(context.Background(), srv.URL)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = stream.Close()
	})

	ev, err := stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, Event{
		ID:   "",
		Type: "message",
		Data: "",
	}, ev)

	_, err = stream.Recv()
	assert.ErrorIs(t, err, io.EOF)
}

func TestStream_DataFieldWithColonAndNoValueDispatchesEmptyData(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data:\n\n")
	}))
	t.Cleanup(srv.Close)

	c := New(WithHTTPClient(srv.Client()))
	stream, err := c.OpenURL(context.Background(), srv.URL)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = stream.Close()
	})

	ev, err := stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, Event{
		ID:   "",
		Type: "message",
		Data: "",
	}, ev)

	_, err = stream.Recv()
	assert.ErrorIs(t, err, io.EOF)
}

func TestStream_ParsesFieldsWithoutOptionalSpaceAfterColon(t *testing.T) {
	t.Parallel()

	body := "id:abc123\n" +
		"event:update\n" +
		"retry:2500\n" +
		"data:payload\n" +
		"\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)

	c := New(WithHTTPClient(srv.Client()))
	stream, err := c.OpenURL(context.Background(), srv.URL)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = stream.Close()
	})

	ev, err := stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, Event{
		ID:   "abc123",
		Type: "update",
		Data: "payload",
	}, ev)
	assert.Equal(t, State{
		LastEventID: "abc123",
		Retry:       2500 * time.Millisecond,
	}, stream.State())

	_, err = stream.Recv()
	assert.ErrorIs(t, err, io.EOF)
}

func TestStream_CommentOnlyBlockDoesNotDispatch(t *testing.T) {
	t.Parallel()

	body := ": keepalive\n\n" +
		"data: payload\n\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)

	c := New(WithHTTPClient(srv.Client()))
	stream, err := c.OpenURL(context.Background(), srv.URL)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = stream.Close()
	})

	ev, err := stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, Event{
		ID:   "",
		Type: "message",
		Data: "payload",
	}, ev)

	_, err = stream.Recv()
	assert.ErrorIs(t, err, io.EOF)
}

func TestStream_LargeDataPayload(t *testing.T) {
	t.Parallel()

	payload := strings.Repeat("x", 256*1024)
	body := "data: " + payload + "\n\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)

	c := New(WithHTTPClient(srv.Client()))
	stream, err := c.OpenURL(context.Background(), srv.URL)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = stream.Close()
	})

	ev, err := stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, Event{
		ID:   "",
		Type: "message",
		Data: payload,
	}, ev)

	_, err = stream.Recv()
	assert.ErrorIs(t, err, io.EOF)
}

func TestStream_FieldOrderingAndLastValueWins(t *testing.T) {
	t.Parallel()

	body := "data: payload\n" +
		"id: first\n" +
		"id: second\n" +
		"retry: 1000\n" +
		"retry: invalid\n" +
		"retry: 2000\n" +
		"event: update\n" +
		"\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)

	c := New(WithHTTPClient(srv.Client()))
	stream, err := c.OpenURL(context.Background(), srv.URL)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = stream.Close()
	})

	ev, err := stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, Event{
		ID:   "second",
		Type: "update",
		Data: "payload",
	}, ev)
	assert.Equal(t, State{
		LastEventID: "second",
		Retry:       2 * time.Second,
	}, stream.State())

	_, err = stream.Recv()
	assert.ErrorIs(t, err, io.EOF)
}

func TestStream_NullInIDDoesNotOverwriteLastEventID(t *testing.T) {
	t.Parallel()

	body := "id: keep\n" +
		"data: first\n" +
		"\n" +
		"id: bad\x00id\n" +
		"data: second\n" +
		"\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)

	c := New(WithHTTPClient(srv.Client()))
	stream, err := c.OpenURL(context.Background(), srv.URL)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = stream.Close()
	})

	ev, err := stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, Event{
		ID:   "keep",
		Type: "message",
		Data: "first",
	}, ev)

	ev, err = stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, Event{
		ID:   "keep",
		Type: "message",
		Data: "second",
	}, ev)
	assert.Equal(t, State{
		LastEventID: "keep",
	}, stream.State())

	_, err = stream.Recv()
	assert.ErrorIs(t, err, io.EOF)
}

func TestStream_CarriageReturnOnlyLineEndings(t *testing.T) {
	t.Parallel()

	body := "data: line1\r" +
		"data: line2\r" +
		"\r"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)

	c := New(WithHTTPClient(srv.Client()))
	stream, err := c.OpenURL(context.Background(), srv.URL)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = stream.Close()
	})

	ev, err := stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, Event{
		ID:   "",
		Type: "message",
		Data: "line1\nline2",
	}, ev)

	_, err = stream.Recv()
	assert.ErrorIs(t, err, io.EOF)
}

func TestParseRetry_ASCIIDigitsOnly(t *testing.T) {
	t.Parallel()

	d, ok := parseRetry("1500")
	require.True(t, ok)
	assert.Equal(t, 1500*time.Millisecond, d)

	d, ok = parseRetry("+1500")
	assert.False(t, ok)
	assert.Zero(t, d)
}
