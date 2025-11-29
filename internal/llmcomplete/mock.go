package llmcomplete

import (
	"fmt"
	"strings"
)

type mockConversation struct {
	conversation
	responses map[string]string
}

var _ Conversation = (*mockConversation)(nil) // ensure mockConversation is a Conversation

// NewMockConversation returns a mock conversation that replies with the value for any key contained in the user message.
func NewMockConversation(modelID ModelID, systemMessage string, responses map[string]string) Conversation {
	return &mockConversation{
		conversation: conversation{
			model: modelOrDefault(modelID),
			messages: []*Message{
				{Role: RoleSystem, Text: systemMessage},
			},
		},
		responses: responses,
	}
}

// Send checks the last user message for any keyword in responses and returns the associated reply.
func (c *mockConversation) Send() (*Message, error) {
	if len(c.messages) < 2 || c.messages[0].Role != RoleSystem {
		return nil, fmt.Errorf("invalid conversation state")
	}
	lastUserMessage := c.LastMessage()
	if lastUserMessage.Role != RoleUser {
		return nil, fmt.Errorf("in order to send, the last message in the Conversation must be a user message")
	}
	lower := strings.ToLower(lastUserMessage.Text)
	for k, resp := range c.responses {
		if strings.Contains(lower, strings.ToLower(k)) {
			m := &Message{Role: RoleAssistant, Text: resp}
			c.messages = append(c.messages, m)
			return m, nil
		}
	}
	err := fmt.Errorf("no mock response for %q", lastUserMessage.Text)
	lastUserMessage.Errors = append(lastUserMessage.Errors, &ResponseError{Error: err, Message: err.Error()})
	return nil, err
}
