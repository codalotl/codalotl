package llmstream

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"testing"

	"github.com/codalotl/codalotl/internal/llmmodel"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPromptCacheKey_ComputedAndStoredOnConversation(t *testing.T) {
	sc := NewConversation(llmmodel.ModelID("gpt-4o-mini"), "system prompt").(*streamingConversation)

	require.NotEmpty(t, sc.promptCacheKey)

	// sha256 hex
	require.Len(t, sc.promptCacheKey, 64)
	require.True(t, regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(sc.promptCacheKey))
}

func TestPromptCacheKeyFromReader_Deterministic(t *testing.T) {
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i)
	}

	got, err := newPromptCacheKeyFromReader(bytes.NewReader(seed))
	require.NoError(t, err)

	sum := sha256.Sum256(seed)
	want := hex.EncodeToString(sum[:])
	assert.Equal(t, want, got)
}

func TestOpenAIResponsesParams_IncludePromptCacheKey(t *testing.T) {
	sc := NewConversation(llmmodel.ModelID("gpt-4o-mini"), "system prompt").(*streamingConversation)
	require.NoError(t, sc.AddUserTurn("hello"))

	req, err := sc.buildOpenAIResponsesParams(llmmodel.ModelInfo{ProviderModelID: "gpt-4o-mini"})
	require.NoError(t, err)

	b, err := json.Marshal(req)
	require.NoError(t, err)

	// We assert via JSON so the test is resilient to openai-go's internal param wrapper types.
	s := string(b)
	assert.Contains(t, s, "prompt_cache_key")
	assert.Contains(t, s, sc.promptCacheKey)
}
