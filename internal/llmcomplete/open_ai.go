package llmcomplete

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/codalotl/codalotl/internal/llmcomplete/internal/modellist"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

func getEnvWithPossibleDollar(key string) string {
	if key == "" {
		return ""
	}
	envVar := strings.TrimPrefix(key, "$")
	if envVar != "" {
		if v := os.Getenv(envVar); v != "" {
			return v
		}
	}
	return ""
}

func getClientOpenAI(provider *modellist.Provider, overrides ModelOverrides) *openai.Client {
	var apiKey string

	// Priority 1: overrides api key
	apiKey = overrides.APIActualKey

	// Priority 2: overrides api key env
	if apiKey == "" {
		apiKey = getEnvWithPossibleDollar(overrides.APIKeyEnv)
	}

	// Priority 3: configured key from configuredProviderKeys
	if apiKey == "" {
		if provider != nil {
			apiKey = configuredProviderKeys[provider.ID]
		}
	}

	// Priority 4: provider's APIKeyEnv
	if apiKey == "" {
		if provider != nil {
			apiKey = getEnvWithPossibleDollar(provider.APIKeyEnv)
		}
	}

	// Priority last:
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}

	if apiKey == "" {
		return nil
	}

	// Resolve endpoint URL: prefer overrides' env/url, then provider's env/url.
	var baseURL string

	// Priority 1: overrides
	baseURL = overrides.APIEndpointURL

	// Priority 2: provider's env
	if baseURL == "" {
		if provider != nil {
			baseURL = getEnvWithPossibleDollar(provider.APIEndpointEnv)
		}
	}

	// Priority 3: provider's URL
	if baseURL == "" && provider != nil {
		baseURL = provider.APIEndpointURL
	}

	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithMaxRetries(0),
	}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	client := openai.NewClient(opts...)
	return &client
}

func setRateLimitsFromHeaders(rateLimits *RateLimits, headers http.Header) {
	if rateLimits == nil || headers == nil {
		return
	}

	rateLimits.TokensLimit = parseRateLimitInt(headers.Get("x-ratelimit-limit-tokens"))
	rateLimits.RequestsLimit = parseRateLimitInt(headers.Get("x-ratelimit-limit-requests"))
	rateLimits.TokensRemaining = parseRateLimitInt(headers.Get("x-ratelimit-remaining-tokens"))
	rateLimits.RequestsRemaining = parseRateLimitInt(headers.Get("x-ratelimit-remaining-requests"))
	rateLimits.TokensResetsAt = parseRateLimitReset(headers.Get("x-ratelimit-reset-tokens"))
	rateLimits.RequestsResetsAt = parseRateLimitReset(headers.Get("x-ratelimit-reset-requests"))
}

func parseRateLimitInt(val string) int {
	if val == "" {
		return 0
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		return 0
	}
	return n
}

func parseRateLimitReset(val string) time.Time {
	if val == "" {
		return time.Now()
	}
	if d, err := time.ParseDuration(val); err == nil {
		return time.Now().Add(d)
	}
	// Fall back to now when the header is present but not a valid duration.
	return time.Now()
}

func (c *conversation) sendOpenAI() (*Message, error) {
	if c.model.providerObj == nil {
		return nil, fmt.Errorf("sendOpenAI: no provider")
	}
	if c.model.providerObj.Type != modellist.TypeOpenAI {
		return nil, fmt.Errorf("sendOpenAI: provider is not of type OpenAI")
	}
	client := getClientOpenAI(c.model.providerObj, c.model.ModelOverrides)
	if client == nil {
		return nil, fmt.Errorf("could not get client; likely no API key")
	}

	lastUserMessage := c.LastMessage()

	if lastUserMessage.chosenModel == "" {
		lastUserMessage.chosenModel = c.model.modelID
	}

	messages := make([]openai.ChatCompletionMessageParamUnion, 0, len(c.messages))
	for _, msg := range c.messages {
		switch msg.Role {
		case RoleSystem:
			messages = append(messages, openai.SystemMessage(msg.Text))
		case RoleUser:
			messages = append(messages, openai.UserMessage(msg.Text))
		case RoleAssistant:
			messages = append(messages, openai.AssistantMessage(msg.Text))
		default:
			return nil, fmt.Errorf("sendOpenAI: unsupported role %s", msg.Role.String())
		}
	}

	request := openai.ChatCompletionNewParams{
		Model:    openai.ChatModel(lastUserMessage.chosenModel),
		Messages: messages,
	}

	if c.model.ReasoningEffort != "" {
		request.ReasoningEffort = openai.ReasoningEffort(c.model.ReasoningEffort)
	}

	ctx := context.Background()
	var httpResp *http.Response
	resp, err := client.Chat.Completions.New(ctx, request, option.WithResponseInto(&httpResp))
	if err == nil {
		if resp == nil {
			return nil, fmt.Errorf("chat completion response is nil")
		}
		if len(resp.Choices) != 1 {
			return nil, fmt.Errorf("unexpected choices length: %d", len(resp.Choices))
		}

		choice := resp.Choices[0]
		if role := string(choice.Message.Role); role != "assistant" {
			return nil, fmt.Errorf("unexpected role of last message: %s", role)
		}

		text := choice.Message.Content
		if text == "" {
			text = choice.Message.Refusal
		}
		message := &Message{
			Role: RoleAssistant,
			Text: text,
		}

		responseMetadata := &ResponseMetadata{
			RequestID:    resp.ID,
			StopReason:   choice.FinishReason,
			Model:        resp.Model,
			TotalTokens:  int(resp.Usage.TotalTokens),
			InputTokens:  int(resp.Usage.PromptTokens),
			OutputTokens: int(resp.Usage.CompletionTokens),
		}

		if resp.Usage.JSON.CompletionTokensDetails.Valid() {
			responseMetadata.ReasoningTokens = int(resp.Usage.CompletionTokensDetails.ReasoningTokens)
		}

		if httpResp != nil {
			setRateLimitsFromHeaders(&responseMetadata.RateLimits, httpResp.Header)
		}

		message.ResponseMetadata = responseMetadata
		c.messages = append(c.messages, message)

		return message, nil
	}

	responseErr := &ResponseError{Error: err}
	if httpResp != nil {
		setRateLimitsFromHeaders(&responseErr.RateLimits, httpResp.Header)
	}
	lastUserMessage.Errors = append(lastUserMessage.Errors, responseErr)

	retErr := err
	switch e := err.(type) {
	case *openai.Error:
		responseErr.Message = e.Message
		responseErr.StatusCode = e.StatusCode
		if e.StatusCode == 429 || (e.StatusCode >= 500 && e.StatusCode <= 599) {
			retErr = makeRetryable(err)
		}
	default:
		var netErr net.Error
		if errors.As(err, &netErr) {
			responseErr.Message = err.Error()
			retErr = makeRetryable(err)
		} else {
			responseErr.Message = err.Error()
		}
	}

	if responseErr.StatusCode == 0 && httpResp != nil {
		responseErr.StatusCode = httpResp.StatusCode
	}

	return nil, retErr
}
