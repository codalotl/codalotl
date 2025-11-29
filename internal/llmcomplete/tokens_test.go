package llmcomplete

import (
	"math"
	"strings"
	"testing"
)

func TestCountTokensSpecificModel(t *testing.T) {
	testCases := []struct {
		name           string
		text           string
		modelID        ModelID
		expectedTokens int
	}{
		{
			name:           "simple case with known model",
			text:           "hello world",
			modelID:        ModelIDGPT5,
			expectedTokens: 2,
		},
		{
			name:           "another known model",
			text:           "hello world",
			modelID:        ModelIDGrok4,
			expectedTokens: 2,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actualTokens := CountTokensSpecificModel(tc.text, tc.modelID)
			if actualTokens != tc.expectedTokens {
				t.Errorf("CountTokensSpecificModel(%q, %v) = %d; want %d", tc.text, tc.modelID, actualTokens, tc.expectedTokens)
			}
		})
	}

	t.Run("anthropic multiplier applies 1.2x rounding", func(t *testing.T) {
		text := strings.Repeat("hello world ", 25)
		base := CountTokensSpecificModel(text, ModelIDGPT5)
		anth := CountTokensSpecificModel(text, ModelIDClaudeSonnet4)
		expected := int(math.Round(float64(base) * 1.2))

		if anth != expected {
			t.Fatalf("anthropic token count mismatch: base=%d expected=round(base*1.2)=%d got=%d", base, expected, anth)
		}
		if base == expected {
			t.Fatalf("test setup produced no change after multiplier: base=%d expected=%d; choose different text", base, expected)
		}
	})
}
