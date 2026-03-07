package gemini

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
	sse         *sseclient.Stream
	mu          sync.Mutex
	stopped     bool
	lastEventID string
}

func newStream(sse *sseclient.Stream) *Stream {
	return &Stream{sse: sse}
}

// Recv blocks until the next stream event or end-of-stream. Returns io.EOF after interaction.complete or error.
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

	s.mu.Lock()
	if event.EventID != "" {
		s.lastEventID = event.EventID
	}
	if event.Type == EventTypeInteractionComplete || event.Type == EventTypeError {
		s.stopped = true
	}
	s.mu.Unlock()

	return event, nil
}

// Close closes the stream body. Idempotent.
func (s *Stream) Close() error {
	return s.sse.Close()
}

// Response returns HTTP response metadata.
func (s *Stream) Response() *http.Response {
	return s.sse.Response()
}

// LastEventID returns the most recent JSON event_id observed on the stream.
func (s *Stream) LastEventID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastEventID
}

func decodeEvent(sseEvent sseclient.Event) (Event, error) {
	raw := json.RawMessage([]byte(sseEvent.Data))

	var envelope struct {
		EventType string `json:"event_type"`
		EventID   string `json:"event_id"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return Event{}, fmt.Errorf("gemini: decode event envelope: %w", err)
	}

	eventType := sseEvent.Type
	if eventType == "" || eventType == "message" {
		eventType = envelope.EventType
	}

	event := Event{
		Type:    EventType(eventType),
		Raw:     raw,
		EventID: envelope.EventID,
	}

	switch event.Type {
	case EventTypeInteractionStart, EventTypeInteractionComplete:
		var payload struct {
			EventID     string      `json:"event_id"`
			Interaction Interaction `json:"interaction"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			return Event{}, fmt.Errorf("gemini: decode %s: %w", event.Type, err)
		}
		event.EventID = payload.EventID
		event.Interaction = &payload.Interaction

	case EventTypeInteractionStatusUpdate:
		var payload struct {
			EventID       string `json:"event_id"`
			InteractionID string `json:"interaction_id"`
			Status        string `json:"status"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			return Event{}, fmt.Errorf("gemini: decode %s: %w", event.Type, err)
		}
		event.EventID = payload.EventID
		event.InteractionID = payload.InteractionID
		event.Status = payload.Status

	case EventTypeContentStart:
		var payload struct {
			EventID string  `json:"event_id"`
			Content Content `json:"content"`
			Index   int     `json:"index"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			return Event{}, fmt.Errorf("gemini: decode %s: %w", event.Type, err)
		}
		event.EventID = payload.EventID
		event.Content = &payload.Content
		event.Index = payload.Index

	case EventTypeContentDelta:
		var payload struct {
			EventID string `json:"event_id"`
			Delta   Delta  `json:"delta"`
			Index   int    `json:"index"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			return Event{}, fmt.Errorf("gemini: decode %s: %w", event.Type, err)
		}
		event.EventID = payload.EventID
		event.Delta = &payload.Delta
		event.Index = payload.Index

	case EventTypeContentStop:
		var payload struct {
			EventID string `json:"event_id"`
			Index   int    `json:"index"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			return Event{}, fmt.Errorf("gemini: decode %s: %w", event.Type, err)
		}
		event.EventID = payload.EventID
		event.Index = payload.Index

	case EventTypeError:
		var payload struct {
			EventID string   `json:"event_id"`
			Error   APIError `json:"error"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			return Event{}, fmt.Errorf("gemini: decode %s: %w", event.Type, err)
		}
		event.EventID = payload.EventID
		event.Error = &payload.Error

	default:
		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			return Event{}, fmt.Errorf("gemini: decode %s: %w", event.Type, err)
		}
	}

	return event, nil
}
