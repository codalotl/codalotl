package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/codalotl/codalotl/internal/q/sseclient"
)

// Stream decodes SSE events for one streaming request.
type Stream struct {
	sse     *sseclient.Stream
	mu      sync.Mutex
	stopped bool
}

func newStream(sse *sseclient.Stream) *Stream {
	return &Stream{sse: sse}
}

// Recv blocks until next stream event or end-of-stream. Returns io.EOF after message_stop.
func (s *Stream) Recv() (Event, error) {
	return s.RecvContext(context.Background())
}

// RecvContext is like Recv but with per-call cancellation/deadline control.
func (s *Stream) RecvContext(ctx context.Context) (Event, error) {
	s.mu.Lock()
	stopped := s.stopped
	s.mu.Unlock()
	if stopped {
		return Event{}, io.EOF
	}

	sseEvent, err := s.sse.RecvContext(ctx)
	if err != nil {
		return Event{}, err
	}

	event, err := decodeEvent(sseEvent)
	if err != nil {
		return Event{}, err
	}
	if event.Type == EventTypeMessageStop {
		s.mu.Lock()
		s.stopped = true
		s.mu.Unlock()
	}
	return event, nil
}

// Close closes stream body. Idempotent.
func (s *Stream) Close() error {
	return s.sse.Close()
}

// Response returns HTTP response metadata.
func (s *Stream) Response() *http.Response {
	return s.sse.Response()
}

// RequestID returns request-id response header value.
func (s *Stream) RequestID() string {
	resp := s.Response()
	if resp == nil {
		return ""
	}
	return resp.Header.Get("request-id")
}

func decodeEvent(sseEvent sseclient.Event) (Event, error) {
	raw := json.RawMessage([]byte(sseEvent.Data))
	eventType := sseEvent.Type
	if eventType == "" || eventType == "message" {
		var fallback struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &fallback); err == nil && fallback.Type != "" {
			eventType = fallback.Type
		}
	}

	event := Event{
		Type: EventType(eventType),
		Raw:  raw,
	}

	switch event.Type {
	case EventTypeMessageStart:
		var payload struct {
			Message Message `json:"message"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			return Event{}, fmt.Errorf("anthropic: decode message_start: %w", err)
		}
		event.Message = &payload.Message

	case EventTypeContentBlockStart:
		var payload struct {
			Index        int          `json:"index"`
			ContentBlock ContentBlock `json:"content_block"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			return Event{}, fmt.Errorf("anthropic: decode content_block_start: %w", err)
		}
		event.Index = payload.Index
		event.ContentBlock = &payload.ContentBlock

	case EventTypeContentBlockDelta:
		var payload struct {
			Index int               `json:"index"`
			Delta ContentBlockDelta `json:"delta"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			return Event{}, fmt.Errorf("anthropic: decode content_block_delta: %w", err)
		}
		event.Index = payload.Index
		event.Delta = &payload.Delta

	case EventTypeContentBlockStop:
		var payload struct {
			Index int `json:"index"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			return Event{}, fmt.Errorf("anthropic: decode content_block_stop: %w", err)
		}
		event.Index = payload.Index

	case EventTypeMessageDelta:
		var payload struct {
			Delta struct {
				StopReason   string `json:"stop_reason"`
				StopSequence string `json:"stop_sequence"`
			} `json:"delta"`
			Usage Usage `json:"usage"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			return Event{}, fmt.Errorf("anthropic: decode message_delta: %w", err)
		}
		event.MessageDelta = &MessageDelta{
			StopReason:   payload.Delta.StopReason,
			StopSequence: payload.Delta.StopSequence,
			Usage:        payload.Usage,
		}

	case EventTypeError:
		var payload struct {
			Error APIError `json:"error"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			return Event{}, fmt.Errorf("anthropic: decode error event: %w", err)
		}
		event.Error = &payload.Error

	case EventTypeMessageStop, EventTypePing:
		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			return Event{}, fmt.Errorf("anthropic: decode %s: %w", event.Type, err)
		}

	default:
		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			return Event{}, fmt.Errorf("anthropic: decode %s: %w", event.Type, err)
		}
	}

	return event, nil
}
