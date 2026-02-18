package docubot

import (
	"github.com/codalotl/codalotl/internal/llmcomplete"
	"log/slog"
	"strings"
)

// responsesConversationalist returns canned responses in the order they are requested. It matches any user message and records conversations for inspection when
// needed.
//
// NOTE: This unified implementation supersedes the old orderedResponsesConversationalist, interceptingConversationalist, and flexibleResponsesConversationalist
// types that previously existed in this file. Those names are now kept as type aliases for backward compatibility so the actual test code does not need to change.
type responsesConversationalist struct {
	responses []string
	convs     []*interceptingConversation
}

// allUserText returns the concatenation of all user messages sent across every conversation.
func (o *responsesConversationalist) allUserText() string {
	var b strings.Builder
	for _, c := range o.convs {
		for _, msg := range c.userMessagesText {
			b.WriteString(msg)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// NewConversation implements llmcomplete.Conversationalist by returning a mock conversation that replies with the next canned response.
func (o *responsesConversationalist) NewConversation(model llmcomplete.ModelID, systemMessage string) llmcomplete.Conversation {
	if len(o.responses) == 0 {
		panic("unexpected conversation; add more responses as needed")
	}

	// Pop the next canned response.
	resp := o.responses[0]
	o.responses = o.responses[1:]

	// Match any user message by using an empty key "" which is contained in
	// every string.
	inner := llmcomplete.NewMockConversation(model, systemMessage, map[string]string{"": resp})

	// Always wrap with interceptingConversation so tests can inspect the user
	// messages that are being sent to the LLM.
	ic := &interceptingConversation{inner: inner}
	o.convs = append(o.convs, ic)
	return ic
}

// interceptingConversation wraps a Conversation and records added user messages for inspection.
type interceptingConversation struct {
	inner            llmcomplete.Conversation
	userMessagesText []string
}

func (c *interceptingConversation) LastMessage() *llmcomplete.Message { return c.inner.LastMessage() }

func (c *interceptingConversation) Messages() []*llmcomplete.Message { return c.inner.Messages() }

func (c *interceptingConversation) LastError() *llmcomplete.ResponseError { return c.inner.LastError() }

func (c *interceptingConversation) Usage() []llmcomplete.Usage { return c.inner.Usage() }

func (c *interceptingConversation) SetLogger(l *slog.Logger) { c.inner.SetLogger(l) }

func (c *interceptingConversation) AddUserMessage(msg string) *llmcomplete.Message {
	c.userMessagesText = append(c.userMessagesText, msg)
	return c.inner.AddUserMessage(msg)
}

func (c *interceptingConversation) Send() (*llmcomplete.Message, error) { return c.inner.Send() }
