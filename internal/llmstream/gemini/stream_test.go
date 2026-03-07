package gemini

import (
	"encoding/json"
	"testing"

	"github.com/codalotl/codalotl/internal/q/sseclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodeEvent_ContentDeltaFieldMappings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		delta map[string]any
		check func(t *testing.T, delta *Delta)
	}{
		{
			name:  "text",
			delta: map[string]any{"type": "text", "text": "hello"},
			check: func(t *testing.T, delta *Delta) {
				assert.Equal(t, "text", delta.Type)
				assert.Equal(t, "hello", delta.Text)
			},
		},
		{
			name: "thought_summary",
			delta: map[string]any{
				"type": "thought_summary",
				"content": map[string]any{
					"type": "text",
					"text": "I should call a tool.",
				},
			},
			check: func(t *testing.T, delta *Delta) {
				assert.Equal(t, "thought_summary", delta.Type)
				require.NotNil(t, delta.SummaryContent)
				assert.Equal(t, "text", delta.SummaryContent.Type)
				assert.Equal(t, "I should call a tool.", delta.SummaryContent.Text)
			},
		},
		{
			name:  "thought_signature",
			delta: map[string]any{"type": "thought_signature", "signature": "sig_123"},
			check: func(t *testing.T, delta *Delta) {
				assert.Equal(t, "thought_signature", delta.Type)
				assert.Equal(t, "sig_123", delta.Signature)
			},
		},
		{
			name: "function_call",
			delta: map[string]any{
				"type": "function_call",
				"id":   "call_1",
				"name": "get_weather",
				"arguments": map[string]any{
					"location": "Boston",
				},
			},
			check: func(t *testing.T, delta *Delta) {
				assert.Equal(t, "function_call", delta.Type)
				assert.Equal(t, "call_1", delta.ID)
				assert.Equal(t, "get_weather", delta.Name)
				assert.Equal(t, map[string]any{"location": "Boston"}, delta.Arguments)
			},
		},
		{
			name: "function_result",
			delta: map[string]any{
				"type":     "function_result",
				"call_id":  "call_1",
				"name":     "get_weather",
				"is_error": true,
				"result":   "boom",
			},
			check: func(t *testing.T, delta *Delta) {
				assert.Equal(t, "function_result", delta.Type)
				assert.Equal(t, "call_1", delta.CallID)
				assert.Equal(t, "get_weather", delta.Name)
				assert.Equal(t, true, delta.IsError)
				assert.Equal(t, "boom", delta.Result)
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			raw, err := json.Marshal(map[string]any{
				"event_id":   "evt_1",
				"event_type": "content.delta",
				"index":      2,
				"delta":      tc.delta,
			})
			require.NoError(t, err)

			event, err := decodeEvent(sseclient.Event{
				Type: string(EventTypeContentDelta),
				Data: string(raw),
			})
			require.NoError(t, err)
			require.NotNil(t, event.Delta)

			assert.Equal(t, EventTypeContentDelta, event.Type)
			assert.Equal(t, 2, event.Index)
			assert.Equal(t, "evt_1", event.EventID)
			tc.check(t, event.Delta)
		})
	}
}

func TestDecodeEvent_DefaultMessageEventTypeFallback(t *testing.T) {
	t.Parallel()

	raw := `{"event_id":"evt_2","event_type":"content.stop","index":3}`
	event, err := decodeEvent(sseclient.Event{Data: raw})
	require.NoError(t, err)
	assert.Equal(t, EventTypeContentStop, event.Type)
	assert.Equal(t, 3, event.Index)
	assert.Equal(t, "evt_2", event.EventID)
	assert.True(t, json.Valid(event.Raw))
}

func TestAPIErrorError(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "not_found: missing interaction", (&APIError{Code: "not_found", Message: "missing interaction"}).Error())
	assert.Equal(t, "missing interaction", (&APIError{Message: "missing interaction"}).Error())
	assert.Equal(t, "not_found", (&APIError{Code: "not_found"}).Error())
	assert.Equal(t, "gemini API error", (&APIError{}).Error())
	assert.Equal(t, "<nil>", (*APIError)(nil).Error())
}
