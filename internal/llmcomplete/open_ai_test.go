package llmcomplete

import (
	"fmt"
	"strings"
	"testing"

	"io"
	"net/http"
	"os"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestOpenAI(t *testing.T) {
	if !HasDefaultKey(ProviderIDOpenAI) {
		t.Skip("No OpenAI key in env")
	}

	c := NewConversation(ModelIDGPT5Nano, "Follow user instructions.")
	userMessage := c.AddUserMessage("Say apple")

	messages := c.Messages()
	if assert.Len(t, messages, 2) {
		assert.Equal(t, "Follow user instructions.", messages[0].Text)
		assert.Equal(t, RoleSystem, messages[0].Role)
		assert.Equal(t, "Say apple", messages[1].Text)
		assert.Equal(t, RoleUser, messages[1].Role)
	}

	m, err := c.Send()

	assert.NoError(t, err)
	messages = c.Messages()
	assert.Len(t, messages, 3)
	assert.Equal(t, string(ModelIDGPT5Nano), userMessage.chosenModel)

	if assert.NotNil(t, m) {
		assert.Equal(t, m, messages[2])

		assert.Equal(t, "", m.chosenModel) // chosenModel is only for user messages

		assert.Contains(t, strings.ToLower(m.Text), "apple")
		assert.Equal(t, RoleAssistant, m.Role)
		assert.Len(t, m.Errors, 0)
		if assert.NotNil(t, m.ResponseMetadata) {
			assert.True(t, strings.HasPrefix(m.ResponseMetadata.RequestID, "chatcmpl-"))
			assert.True(t, strings.HasPrefix(m.ResponseMetadata.Model, string(ModelIDGPT5Nano)))
			assert.Equal(t, "stop", m.ResponseMetadata.StopReason)

			assertIntBetween(t, 10, 300, m.ResponseMetadata.TotalTokens)     // 154 when testing
			assertIntBetween(t, 10, 100, m.ResponseMetadata.InputTokens)     // 17 when testing
			assertIntBetween(t, 1, 300, m.ResponseMetadata.OutputTokens)     // 138 when testing
			assertIntBetween(t, 10, 300, m.ResponseMetadata.ReasoningTokens) // 128 when testing

			assert.True(t, m.ResponseMetadata.RateLimits.RequestsLimit > 0)
			assert.True(t, m.ResponseMetadata.RateLimits.TokensLimit > 0)
			assertIntBetween(t, 1, m.ResponseMetadata.RequestsLimit-1, m.ResponseMetadata.RateLimits.RequestsRemaining) // assume that we actually use one from our limit
			assertIntBetween(t, 1, m.ResponseMetadata.TokensLimit-1, m.ResponseMetadata.RateLimits.TokensRemaining)     // NOTE: instead of 1, i subtracted m.ResponseMetadata.TotalTokens, which didn't work :shrug:

			assert.NotZero(t, m.ResponseMetadata.RateLimits.RequestsResetsAt)
			assert.NotZero(t, m.ResponseMetadata.RateLimits.TokensResetsAt)
		}
	}
}

func TestOpenAIFailure(t *testing.T) {
	if !HasDefaultKey(ProviderIDOpenAI) {
		t.Skip("No OpenAI key in env")
	}

	c := NewConversation(ModelIDGPT5Nano, "Follow user instructions.")
	userMessage := c.AddUserMessage("Say apple")
	userMessage.chosenModel = "nonexistantmodel" // causes 404

	m, err := c.Send()

	assert.Nil(t, m)
	assert.NotNil(t, err)
	if assert.Len(t, userMessage.Errors, 1) {
		e := userMessage.Errors[0]

		assert.Contains(t, e.Message, "does not exist")
		assert.Equal(t, 404, e.StatusCode)

		// NOTE: e.RateLimits will be 0's, since it's per model and we did a non-existant model
	}
}

// openAIRetryStubTransport implements http.RoundTripper and simulates a transient 500 error on the first call and a successful chat completion response on the second call.
type openAIRetryStubTransport struct{ calls int }

func (rt *openAIRetryStubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.calls++
	if rt.calls == 1 {
		body := `{"error":{"message":"internal server error","type":"server_error"}}`
		h := make(http.Header)
		h.Set("Content-Type", "application/json")
		return &http.Response{StatusCode: 500, Header: h, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	}

	if rt.calls == 2 {
		body := `{"id":"chatcmpl-test","object":"chat.completion","model":"gpt-5-nano","choices":[{"index":0,"message":{"role":"assistant","content":"apple"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30,"completion_tokens_details":{"reasoning_tokens":5}}}`
		h := make(http.Header)
		h.Set("Content-Type", "application/json")
		// Minimal rate limit headers so parsing succeeds
		h.Set("x-ratelimit-limit-tokens", "1000")
		h.Set("x-ratelimit-remaining-tokens", "900")
		h.Set("x-ratelimit-reset-tokens", "1")
		h.Set("x-ratelimit-limit-requests", "100")
		h.Set("x-ratelimit-remaining-requests", "99")
		h.Set("x-ratelimit-reset-requests", "1")
		return &http.Response{StatusCode: 200, Header: h, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	}

	// Any extra calls return 500 again
	body := `{"error":{"message":"unexpected extra call","type":"server_error"}}`
	h := make(http.Header)
	h.Set("Content-Type", "application/json")
	return &http.Response{StatusCode: 500, Header: h, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

func TestOpenAIRetry_NoNetwork(t *testing.T) {
	// Ensure an API key is present so client construction doesn't panic.
	oldKey := os.Getenv("OPENAI_API_KEY")
	_ = os.Setenv("OPENAI_API_KEY", "test")
	defer func() { _ = os.Setenv("OPENAI_API_KEY", oldKey) }()

	// Stub out default transport to avoid real network calls and control responses.
	oldTransport := http.DefaultTransport
	stub := &openAIRetryStubTransport{}
	http.DefaultTransport = stub
	defer func() { http.DefaultTransport = oldTransport }()

	// Speed up retries during test.
	oldSleeps := retrySleepDurations
	retrySleepDurations = []time.Duration{0, 0, 0}
	defer func() { retrySleepDurations = oldSleeps }()

	c := NewConversation(ModelIDGPT5Nano, "Follow user instructions.")
	userMessage := c.AddUserMessage("Say apple")

	m, err := c.Send()
	assert.NoError(t, err)
	if assert.NotNil(t, m) {
		assert.Equal(t, RoleAssistant, m.Role)
		assert.Contains(t, strings.ToLower(m.Text), "apple")
	}
	// First attempt fails, second succeeds
	assert.Equal(t, 2, stub.calls)
	// The error from the first attempt should be recorded on the user message
	assert.Len(t, userMessage.Errors, 1)
}

func assertIntBetween(t *testing.T, lo, hi, actual int) {
	t.Helper()
	assert.True(t, actual >= lo && actual <= hi, fmt.Sprintf("expected %d to be between %d and %d", actual, lo, hi))
}
