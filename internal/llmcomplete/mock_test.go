package llmcomplete

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewMockConversation(t *testing.T) {
	tests := []struct {
		name          string
		modelID       ModelID
		systemMessage string
		responses     map[string]string
		userMessage   string
		expectedError bool
		expectedReply string
	}{
		{
			name:          "basic response match",
			modelID:       ModelIDO3,
			systemMessage: "You are a helpful assistant",
			responses: map[string]string{
				"hello": "Hi there!",
			},
			userMessage:   "hello",
			expectedError: false,
			expectedReply: "Hi there!",
		},
		{
			name:          "case insensitive match",
			modelID:       ModelIDO3,
			systemMessage: "You are a helpful assistant",
			responses: map[string]string{
				"hello": "Hi there!",
			},
			userMessage:   "HELLO",
			expectedError: false,
			expectedReply: "Hi there!",
		},
		{
			name:          "no response match",
			modelID:       ModelIDO3,
			systemMessage: "You are a helpful assistant",
			responses: map[string]string{
				"hello": "Hi there!",
			},
			userMessage:   "goodbye",
			expectedError: true,
			expectedReply: "",
		},
		{
			name:          "partial word match",
			modelID:       ModelIDO3,
			systemMessage: "You are a helpful assistant",
			responses: map[string]string{
				"hello": "Hi there!",
			},
			userMessage:   "hello world",
			expectedError: false,
			expectedReply: "Hi there!",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewMockConversation(tt.modelID, tt.systemMessage, tt.responses)

			// Verify initial state
			messages := c.Messages()
			assert.Len(t, messages, 1)
			assert.Equal(t, RoleSystem, messages[0].Role)
			assert.Equal(t, tt.systemMessage, messages[0].Text)

			// Add user message
			c.AddUserMessage(tt.userMessage)

			// Send and check response
			response, err := c.Send()
			if tt.expectedError {
				assert.Error(t, err)
				assert.Nil(t, response)
				lastError := c.LastError()
				assert.NotNil(t, lastError)
				assert.Contains(t, lastError.Message, tt.userMessage)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, response)
				assert.Equal(t, RoleAssistant, response.Role)
				assert.Equal(t, tt.expectedReply, response.Text)
			}
		})
	}
}

func TestNewMockConversationalist(t *testing.T) {
	tests := []struct {
		name          string
		responses     map[string]string
		modelID       ModelID
		systemMessage string
		userMessage   string
		expectedReply string
	}{
		{
			name:    "basic conversation creation",
			modelID: ModelIDO3,
			responses: map[string]string{
				"hello": "Hi there!",
			},
			systemMessage: "You are a helpful assistant",
			userMessage:   "hello",
			expectedReply: "Hi there!",
		},
		{
			name:          "empty responses",
			modelID:       ModelIDO3,
			responses:     map[string]string{},
			systemMessage: "You are a helpful assistant",
			userMessage:   "hello",
			expectedReply: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conv := NewMockConversationalist(tt.responses)
			assert.NotNil(t, conv)

			// Create a new c
			c := conv.NewConversation(tt.modelID, tt.systemMessage)
			assert.NotNil(t, c)

			// Add user message
			c.AddUserMessage(tt.userMessage)

			// Send and check response
			response, err := c.Send()
			if tt.expectedReply == "" {
				assert.Error(t, err)
				assert.Nil(t, response)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, response)
				assert.Equal(t, RoleAssistant, response.Role)
				assert.Equal(t, tt.expectedReply, response.Text)
			}
		})
	}
}
