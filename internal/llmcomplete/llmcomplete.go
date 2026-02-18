// llmcomplete is a barebones package to abstract LLM completions across multiple providers (OpenAI, Anthropic, etc). It purposefully does NOT take advantage of
// each provider's special features and gimmicks. For instance, there is no tool support. It only does completions.
//
// If a feature (ex: tools) becomes ubiquitous across all providers, we could consider adding it. At the same time, this package is really intended to be basic text
// completion. I'm inclined to forgo adding extra features like that. If we need that kind of stuff, we could consider using OSS components.
package llmcomplete

import (
	"errors"
	"fmt"
	"github.com/codalotl/codalotl/internal/llmcomplete/internal/modellist"
	"github.com/codalotl/codalotl/internal/q/health"
	"log/slog"
	"strings"
	"time"
)

//
// TODOs:
// - If we know about rate limits, we may already know a request will fail. We could store this type of info in this package.
//   counterpoint: rate limit is likely across processes (eg, cli invokations), so we won't in fact know the rate limit in all cases. Do we call the rate limit API to get it?
// - Where do I store clients?
// - auto retry/backoff.
// - maybe ctx context.Context
// - ability to limit response tokens

type Conversation interface {
	LastMessage() *Message
	Messages() []*Message
	AddUserMessage(message string) *Message
	Send() (*Message, error)
	LastError() *ResponseError

	// Usage returns usage for all assistant messages.
	Usage() []Usage

	SetLogger(logger *slog.Logger)
}

type conversation struct {
	model    model
	messages []*Message
	health.Ctx
}

type Role int

const (
	RoleUser Role = iota
	RoleSystem
	RoleAssistant
)

// String returns the string representation of the Role.
func (r Role) String() string {
	switch r {
	case RoleUser:
		return "User"
	case RoleSystem:
		return "System"
	case RoleAssistant:
		return "Assistant"
	default:
		return "Unknown"
	}
}

type Message struct {
	Role             Role
	Text             string
	ResponseMetadata *ResponseMetadata // only set when Role=RoleAssistant
	Errors           []*ResponseError  // only set when Role=RoleUser AND the provider errored or rejected the request
	chosenModel      string            // when Role=RoleUser, the actual model chosen to send this user message
}

type ResponseError struct {
	Error      error // actual error we get from the client library when creating a completion request
	StatusCode int   // HTTP status code
	Message    string
	RateLimits
}

type ResponseMetadata struct {
	//
	// Basics:
	//

	RequestID  string // ex: "chatcmpl-BXYJ0U9PpC3uDzeoP2ZN1nBthfnpu"
	Model      string // ex: "o3-2025-04-16"
	StopReason string // ex: "stop" -- pass-through of the provider's stop/finish reason (e.g., OpenAI "finish_reason", Anthropic "stop_reason")

	//
	// Tokens:
	//

	TotalTokens     int
	InputTokens     int
	ReasoningTokens int
	OutputTokens    int // total output tokens (includes reasoning tokens)
	RateLimits
}

type RateLimits struct {
	TokensLimit       int
	RequestsLimit     int
	TokensRemaining   int
	RequestsRemaining int
	TokensResetsAt    time.Time
	RequestsResetsAt  time.Time
}

// Usage captures token and cost information for an assistant message.
type Usage struct {
	Model           string
	TotalTokens     int
	InputTokens     int
	ReasoningTokens int
	OutputTokens    int
	Cost            float64
	RateLimits
}

// costPerMFor returns the pricing (per 1M tokens) for the given model ID within the conversation's provider. It tries exact match, then a normalized ID (date/latest
// suffix removed). If not found, it falls back to the configured model for the conversation. ok=false if no pricing data is available.
func (c *conversation) costPerMFor(modelID string) (in float64, out float64, ok bool) {
	// Try to resolve within the provider used by this conversation.
	if c.model.providerObj != nil {
		// Exact match
		for i := range c.model.providerObj.Models {
			m := c.model.providerObj.Models[i]
			if m.ID == modelID {
				return m.CostPer1MIn, m.CostPer1MOut, true
			}
		}
		// Normalized match (strip date / latest suffixes)
		normalized := normalizeModelForCost(modelID)
		if normalized != modelID {
			for i := range c.model.providerObj.Models {
				m := c.model.providerObj.Models[i]
				if m.ID == normalized {
					return m.CostPer1MIn, m.CostPer1MOut, true
				}
			}
		}
	}

	// Fall back to the configured model for this conversation, if present.
	if c.model.modelObj != nil {
		return c.model.modelObj.CostPer1MIn, c.model.modelObj.CostPer1MOut, true
	}

	return 0, 0, false
}

func NewConversation(modelID ModelID, systemMessage string) Conversation {
	model, _ := getModelByID(modelID) // NOTE: may be invalid model
	return &conversation{
		model: model,
		messages: []*Message{
			{Role: RoleSystem, Text: systemMessage},
		},
		Ctx: health.NewCtx(slog.New(slog.DiscardHandler)),
	}
}

// ErrRetryable marks an error as retryable by the caller.
var ErrRetryable = errors.New("llmcomplete: retryable")

func makeRetryable(err error) error { return fmt.Errorf("%w: %w", ErrRetryable, err) }
func isRetryable(err error) bool    { return errors.Is(err, ErrRetryable) }

// retrySleepDurations' i'th index is the sleep duration for the i'th retry. Any retry after that would use the last value.
//
// This is meant to mix exponential backoff, an eager initial retry, keeping sleep times long enough that things might recover but short enough that the user doesn't
// think things hung.
var retrySleepDurations = []time.Duration{
	10 * time.Millisecond,
	500 * time.Millisecond,
	1 * time.Second,
	2 * time.Second,
	4 * time.Second,
	10 * time.Second,
}

func (c *conversation) Messages() []*Message {
	return c.messages
}

func (c *conversation) LastMessage() *Message {
	return c.messages[len(c.messages)-1]
}

func (c *conversation) AddUserMessage(message string) *Message {
	m := &Message{
		Role: RoleUser,
		Text: message,
	}
	c.messages = append(c.messages, m)
	return m
}

// Send sends the conversation to the model to get a RoleAssistant response message. The last message in Messages MUST be a UserMessage. If the request errors out,
// an error is returned. Additionally, the last UserMessage will contain details in a ResponseError struct.
func (c *conversation) Send() (*Message, error) {
	if c.model == (model{}) {
		return nil, c.LogNewErr("conversation.Send: invalid model")
	}
	if c.model.providerObj == nil {
		return nil, c.LogNewErr("conversation.Send: no provider record (providerID: %s)", c.model.providerID)
	}
	if len(c.messages) < 2 {
		return nil, c.LogNewErr("in order to send, the Conversation must contain a system and user message")
	}
	if c.messages[0].Role != RoleSystem {
		return nil, c.LogNewErr("in order to send, the first message in the Conversation must be a system message")
	}
	lastUserMessage := c.LastMessage()
	if lastUserMessage.Role != RoleUser {
		return nil, c.LogNewErr("in order to send, the last message in the Conversation must be a user message")
	}

	if c.Logger != nil {
		// Find the last assistant message index
		lastAssistantIdx := -1
		for i := len(c.messages) - 1; i >= 0; i-- {
			if c.messages[i].Role == RoleAssistant {
				lastAssistantIdx = i
				break
			}
		}

		// Start logging from after the last assistant message, or from the beginning if none
		startIdx := 0
		if lastAssistantIdx >= 0 {
			startIdx = lastAssistantIdx + 1
		}

		if startIdx != 0 {
			c.Log("Sending with previous assistant response. All prev messages sent to provider. Logging new messages, starting with index", "index", startIdx)
		}

		// Log messages since last assistant response
		for i := startIdx; i < len(c.messages); i++ {
			m := c.messages[i]
			txtLen := len(m.Text)
			c.Log("conversation.message", "model", c.model.id, "role", m.Role, "bytes", txtLen, "toks", tokenEstimate(txtLen), "multiline", m.Text)
		}
	}

	var newMessage *Message
	var err error

	const retryMaxAttempts = 3

	for attempt := 1; attempt <= retryMaxAttempts; attempt++ {
		switch c.model.providerObj.Type {
		case modellist.TypeOpenAI:
			newMessage, err = c.sendOpenAI()
		case modellist.TypeAnthropic:
			newMessage, err = c.sendAnthropic()
		case modellist.TypeGemini:
			newMessage, err = c.sendGemini()
		default:
			newMessage = nil
			err = fmt.Errorf("provider type %s not implemented", c.model.providerObj.Type)
		}

		if err == nil {
			break
		}

		if isRetryable(err) && attempt < retryMaxAttempts {
			sleep := 0 * time.Millisecond
			if (attempt - 1) < len(retrySleepDurations) {
				sleep = retrySleepDurations[attempt-1]
			} else {
				sleep = retrySleepDurations[len(retrySleepDurations)-1]
			}

			c.Log("conversation.retry", "attempt", attempt, "max", retryMaxAttempts, "sleep", sleep, "err", err.Error())
			time.Sleep(sleep)
			continue
		}

		// Not retryable or out of attempts
		break
	}

	if err != nil {
		return newMessage, c.LogWrappedErr("conversation.send", err)
	}

	txtLen := len(newMessage.Text)
	c.Log("conversation.response", "role", newMessage.Role, "bytes", txtLen, "chosenModel", lastUserMessage.chosenModel, "multiline", newMessage.Text)
	usages := c.Usage()
	c.Log("conversation.usage", usages[len(usages)-1].LogPairs()...)

	return newMessage, nil
}

// Returns nil if the last message isn't a User message with an error. Otherwise, returns the last element in Errors.
func (c *conversation) LastError() *ResponseError {
	lastMsg := c.LastMessage()
	if lastMsg.Role == RoleUser && len(lastMsg.Errors) > 0 {
		return lastMsg.Errors[len(lastMsg.Errors)-1]
	}
	return nil
}

func (c *conversation) Usage() []Usage {
	var usages []Usage
	for _, m := range c.messages {
		if m.Role != RoleAssistant || m.ResponseMetadata == nil {
			continue
		}

		responseMetadata := m.ResponseMetadata
		u := Usage{
			Model:           responseMetadata.Model,
			TotalTokens:     responseMetadata.TotalTokens,
			InputTokens:     responseMetadata.InputTokens,
			ReasoningTokens: responseMetadata.ReasoningTokens,
			OutputTokens:    responseMetadata.OutputTokens,
			RateLimits:      responseMetadata.RateLimits,
		}

		costModelKey := responseMetadata.Model
		inPerM, outPerM, ok := c.costPerMFor(costModelKey)
		if ok {
			u.Cost = float64(u.InputTokens)*(inPerM/1e6) + float64(u.OutputTokens)*(outPerM/1e6)
		} else {
			fmt.Println("WARNING! No model cost for ", responseMetadata.Model)
		}

		usages = append(usages, u)
	}
	return usages
}

func (u Usage) String() string {
	return fmt.Sprintf("model=%s tokens=%d in=%d reasoning=%d out=%d cost=$%.4f token_limits= %d/%d request_limits=%d/%d",
		u.Model, u.TotalTokens, u.InputTokens, u.ReasoningTokens, u.OutputTokens, u.Cost,
		u.RateLimits.TokensRemaining, u.RateLimits.TokensLimit, u.RateLimits.RequestsRemaining, u.RateLimits.RequestsLimit)
}

// LogPairs returns an even number of elements, string and value, for use in slog's logging.
func (u Usage) LogPairs() []any {
	return []any{
		"model", u.Model,
		"tokens", u.TotalTokens,
		"in", u.InputTokens,
		"reasoning", u.ReasoningTokens,
		"out", u.OutputTokens,
		"cost", u.Cost,
		"token_limits", fmt.Sprintf("%d/%d", u.RateLimits.TokensRemaining, u.RateLimits.TokensLimit),
		"request_limits", fmt.Sprintf("%d/%d", u.RateLimits.RequestsRemaining, u.RateLimits.RequestsLimit),
	}
}

func normalizeModelForCost(model string) string {
	parts := strings.Split(model, "-")
	if len(parts) <= 1 {
		return model
	}

	last := parts[len(parts)-1]
	if len(last) == 8 {
		allDigits := true
		for _, r := range last {
			if r < '0' || r > '9' {
				allDigits = false
				break
			}
		}
		if allDigits {
			return strings.Join(parts[:len(parts)-1], "-")
		}
	}

	if last == "latest" {
		return strings.Join(parts[:len(parts)-1], "-")
	}

	return model
}

// PrintTotalUsage prints a compact summary of the total usage slice. If the slice is empty, "usage: none" is printed.
func PrintTotalUsage(usages []Usage) {
	if len(usages) == 0 {
		fmt.Println("usage: none")
		return
	}

	var total Usage
	for _, u := range usages {
		total.TotalTokens += u.TotalTokens
		total.InputTokens += u.InputTokens
		total.ReasoningTokens += u.ReasoningTokens
		total.OutputTokens += u.OutputTokens
		total.Cost += u.Cost
	}
	total.Model = usages[len(usages)-1].Model
	total.RateLimits = usages[len(usages)-1].RateLimits

	fmt.Println("usage: ", total)
}

func (c *conversation) SetLogger(logger *slog.Logger) {
	c.Logger = logger
}

// Go code is approximately 3 bytes per token. English prose is 4 bytes per token. This assumes Go code and can be improved later.
func tokenEstimate(byteCount int) int {
	return byteCount / 3
}
