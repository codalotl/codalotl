package anthropic

import (
	"encoding/json"
	"testing"

	"github.com/codalotl/codalotl/internal/q/sseclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodeEvent_ContentBlockDeltaFieldMappings(t *testing.T) {
	t.Parallel()

	partialJSON := `{"city":"San`
	tests := []struct {
		name  string
		delta map[string]any
		want  ContentBlockDelta
	}{
		{
			name:  "text_delta",
			delta: map[string]any{"type": "text_delta", "text": "hello"},
			want:  ContentBlockDelta{Type: "text_delta", Text: "hello"},
		},
		{
			name:  "thinking_delta",
			delta: map[string]any{"type": "thinking_delta", "thinking": "I should call a tool"},
			want:  ContentBlockDelta{Type: "thinking_delta", Thinking: "I should call a tool"},
		},
		{
			name:  "signature_delta",
			delta: map[string]any{"type": "signature_delta", "signature": "sig_123"},
			want:  ContentBlockDelta{Type: "signature_delta", Signature: "sig_123"},
		},
		{
			name:  "input_json_delta",
			delta: map[string]any{"type": "input_json_delta", "partial_json": partialJSON},
			want:  ContentBlockDelta{Type: "input_json_delta", PartialJSON: partialJSON},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			raw, err := json.Marshal(map[string]any{
				"type":  "content_block_delta",
				"index": 2,
				"delta": tc.delta,
			})
			require.NoError(t, err)

			event, err := decodeEvent(sseclient.Event{
				Type: string(EventTypeContentBlockDelta),
				Data: string(raw),
			})
			require.NoError(t, err)
			require.NotNil(t, event.Delta)

			assert.Equal(t, EventTypeContentBlockDelta, event.Type)
			assert.Equal(t, 2, event.Index)
			assert.Equal(t, tc.want.Type, event.Delta.Type)
			assert.Equal(t, tc.want.Text, event.Delta.Text)
			assert.Equal(t, tc.want.Thinking, event.Delta.Thinking)
			assert.Equal(t, tc.want.PartialJSON, event.Delta.PartialJSON)
			assert.Equal(t, tc.want.Signature, event.Delta.Signature)
		})
	}
}
