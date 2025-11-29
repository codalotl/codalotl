package llmcomplete

import (
	"context"
	"fmt"
	"github.com/codalotl/codalotl/internal/llmcomplete/internal/gemini"
	"github.com/codalotl/codalotl/internal/llmcomplete/internal/modellist"
	"os"
	"strings"
	"time"
)

// geminiResolvedKey mirrors other providers' resolution order for API keys.
// Order:
//  1. overrides.APIActualKey
//  2. env from overrides.APIKeyEnv (supports optional leading $)
//  3. configuredProviderKeys[gemini]
//  4. env from provider.APIKeyEnv (if provider != nil)
//  5. GEMINI_API_KEY
func geminiResolvedKey(provider *modellist.Provider, overrides ModelOverrides) string {
	if overrides.APIActualKey != "" {
		return overrides.APIActualKey
	}
	if v := getEnvWithPossibleDollar(overrides.APIKeyEnv); v != "" {
		return v
	}
	if v := configuredProviderKeys[ProviderIDGemini]; v != "" {
		return v
	}
	if provider != nil {
		if v := getEnvWithPossibleDollar(provider.APIKeyEnv); v != "" {
			return v
		}
	}
	if v := os.Getenv("GEMINI_API_KEY"); v != "" {
		return v
	}
	return ""
}

// geminiResolvedBaseURL mirrors other providers' URL resolution order.
// Order:
//  1. overrides.APIEndpointURL
//  2. env from provider.APIEndpointEnv (if provider != nil)
//  3. provider.APIEndpointURL (if provider != nil)
func geminiResolvedBaseURL(provider *modellist.Provider, overrides ModelOverrides) string {
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

func getClientGemini(provider *modellist.Provider, overrides ModelOverrides) *gemini.Client {
	key := geminiResolvedKey(provider, overrides)
	if key == "" {
		return nil
	}
	client := gemini.NewClient(key)
	if base := geminiResolvedBaseURL(provider, overrides); base != "" {
		client.BaseURL = strings.TrimRight(base, "/")
	}
	return client
}

func (c *conversation) sendGemini() (*Message, error) {
	if c.model.providerObj == nil {
		return nil, fmt.Errorf("sendGemini: no provider")
	}
	if c.model.providerObj.Type != modellist.TypeGemini {
		return nil, fmt.Errorf("sendGemini: provider is not of type Gemini")
	}

	lastUserMessage := c.LastMessage()
	if lastUserMessage.chosenModel == "" {
		lastUserMessage.chosenModel = c.model.modelID
	}

	// Build request payload
	req := gemini.GenerateContentRequest{
		Model: lastUserMessage.chosenModel,
	}

	// System instruction
	if len(c.messages) > 0 && c.messages[0].Role == RoleSystem {
		req.SystemInstruction = &gemini.Content{Parts: []gemini.Part{{Text: c.messages[0].Text}}}
	}

	// Convert conversation messages to Gemini contents. We send the user/assistant turns after the system.
	for idx, m := range c.messages {
		if idx == 0 {
			continue
		}
		var role string
		switch m.Role {
		case RoleUser:
			role = "user"
		case RoleAssistant:
			role = "model"
		default:
			// skip unknown roles
			continue
		}
		req.Contents = append(req.Contents, gemini.Content{Role: role, Parts: []gemini.Part{{Text: m.Text}}})
	}

	client := getClientGemini(c.model.providerObj, c.model.ModelOverrides)
	if client == nil {
		return nil, fmt.Errorf("could not get client; likely no API key")
	}
	ctx := context.Background()
	resp, err := client.GenerateContent(ctx, req)
	if err != nil {
		// Record error on the last user message
		responseErr := &ResponseError{Error: err, Message: err.Error()}
		// Gemini minimal client does not expose ratelimit headers; set reset times to now for visibility
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

	message.Text = strings.TrimSpace(resp.Text())

	// Gemini's response does not include an explicit request ID in this minimal client; leave blank.
	responseMetadata.Model = lastUserMessage.chosenModel
	// Derive a generic stop reason from the first candidate if present.
	if len(resp.Candidates) > 0 {
		responseMetadata.StopReason = resp.Candidates[0].FinishReason
	}

	if resp.UsageMetadata != nil {
		responseMetadata.InputTokens = resp.UsageMetadata.PromptTokenCount
		responseMetadata.OutputTokens = resp.UsageMetadata.CandidatesTokenCount
		responseMetadata.TotalTokens = resp.UsageMetadata.TotalTokenCount
		responseMetadata.ReasoningTokens = 0
	}

	c.messages = append(c.messages, message)
	return message, nil
}
