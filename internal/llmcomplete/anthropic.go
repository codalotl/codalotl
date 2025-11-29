package llmcomplete

import (
	"github.com/codalotl/codalotl/internal/llmcomplete/internal/anthropic"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/codalotl/codalotl/internal/llmcomplete/internal/modellist"
)

// anthropicResolvedKey mirrors getClientOpenAI's resolution order for API keys.
// Order:
//  1. overrides.APIActualKey
//  2. env from overrides.APIKeyEnv (supports optional leading $)
//  3. configuredProviderKeys[anthropic]
//  4. env from provider.APIKeyEnv (if provider != nil)
//  5. ANTHROPIC_API_KEY
func anthropicResolvedKey(provider *modellist.Provider, overrides ModelOverrides) string {
	// 1
	if overrides.APIActualKey != "" {
		return overrides.APIActualKey
	}
	// 2
	if v := getEnvWithPossibleDollar(overrides.APIKeyEnv); v != "" {
		return v
	}
	// 3
	if v := configuredProviderKeys[ProviderIDAnthropic]; v != "" {
		return v
	}
	// 4
	if provider != nil {
		if v := getEnvWithPossibleDollar(provider.APIKeyEnv); v != "" {
			return v
		}
	}
	// 5
	if v := os.Getenv("ANTHROPIC_API_KEY"); v != "" {
		return v
	}
	return ""
}

// anthropicResolvedBaseURL mirrors getClientOpenAI's URL resolution order.
// Order:
//  1. overrides.APIEndpointURL
//  2. env from provider.APIEndpointEnv (if provider != nil)
//  3. provider.APIEndpointURL (if provider != nil)
func anthropicResolvedBaseURL(provider *modellist.Provider, overrides ModelOverrides) string {
	if overrides.APIEndpointURL != "" {
		return overrides.APIEndpointURL
	}
	if provider != nil {
		if v := getEnvWithPossibleDollar(provider.APIEndpointEnv); v != "" {
			return v
		}
	}
	if provider != nil {
		return provider.APIEndpointURL
	}
	return ""
}

func getClientAnthropic(provider *modellist.Provider, overrides ModelOverrides) *anthropic.Client {
	key := anthropicResolvedKey(provider, overrides)
	if key == "" {
		return nil
	}
	client := anthropic.NewClient(key)
	if base := anthropicResolvedBaseURL(provider, overrides); base != "" {
		client.BaseURL = strings.TrimRight(base, "/")
	}
	return client
}

func (c *conversation) sendAnthropic() (*Message, error) {
	if c.model.providerObj == nil {
		return nil, fmt.Errorf("sendAnthropic: no provider")
	}
	if c.model.providerObj.Type != modellist.TypeAnthropic {
		return nil, fmt.Errorf("sendAnthropic: provider is not of type Anthropic")
	}

	lastUserMessage := c.LastMessage()
	if lastUserMessage.chosenModel == "" {
		lastUserMessage.chosenModel = c.model.modelID
	}

	// This is just the max output tokens the API supports of most models on 2025/08/09.
	// Some models (haiku) support fewer tokens.
	maxTokens := 32_000
	maxTokensPerModelOverride := map[string]int{
		"claude-3-5-haiku": 8192,
	}
	if maxTokenForModel, ok := maxTokensPerModelOverride[normalizeModelForCost(lastUserMessage.chosenModel)]; ok {
		maxTokens = maxTokenForModel
	}

	// Build request
	req := anthropic.MessageRequest{
		Model:     lastUserMessage.chosenModel,
		MaxTokens: maxTokens,
	}

	// First message is always system
	if len(c.messages) > 0 && c.messages[0].Role == RoleSystem {
		req.System = c.messages[0].Text
	}
	// Convert remaining messages
	for idx, m := range c.messages {
		if idx == 0 {
			continue
		}
		var role string
		switch m.Role {
		case RoleUser:
			role = "user"
		case RoleAssistant:
			role = "assistant"
		default:
			// Ignore unknown roles
			continue
		}
		req.Messages = append(req.Messages, anthropic.UserMessageRef{Role: role, Content: m.Text})
	}

	client := getClientAnthropic(c.model.providerObj, c.model.ModelOverrides)
	if client == nil {
		return nil, fmt.Errorf("could not get client; likely no API key")
	}
	ctx := context.Background()
	resp, err := client.CreateMessage(ctx, req)
	if err != nil {
		// Record error on the last user message
		responseErr := &ResponseError{Error: err, Message: err.Error()}
		// No HTTP status code surfaced by minimal client
		// No ratelimit headers available; set resets to now for visibility
		responseErr.RateLimits.RequestsResetsAt = time.Now()
		responseErr.RateLimits.TokensResetsAt = time.Now()
		lastUserMessage.Errors = append(lastUserMessage.Errors, responseErr)

		// Mark retryable for typical transient cases based on message text.
		errMsg := strings.ToLower(err.Error())
		if strings.Contains(errMsg, "status 429") || strings.Contains(errMsg, "status 503") || strings.Contains(errMsg, "status 502") || strings.Contains(errMsg, "status 500") {
			return nil, makeRetryable(err)
		}
		return nil, err
	}

	// Success
	responseMetadata := &ResponseMetadata{}
	message := &Message{Role: RoleAssistant, ResponseMetadata: responseMetadata}

	// Text response is concatenated text content blocks
	message.Text = strings.TrimSpace(resp.Text())

	responseMetadata.RequestID = resp.ID
	responseMetadata.Model = resp.Model
	responseMetadata.StopReason = resp.StopReason

	// Tokens:
	responseMetadata.InputTokens = resp.Usage.InputTokens
	responseMetadata.OutputTokens = resp.Usage.OutputTokens
	responseMetadata.TotalTokens = resp.Usage.InputTokens + resp.Usage.OutputTokens
	// Anthropic does not currently expose reasoning tokens in the Messages API response.
	responseMetadata.ReasoningTokens = 0

	c.messages = append(c.messages, message)
	return message, nil
}
