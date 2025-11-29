package llmcomplete

type Conversationalist interface {
	NewConversation(modelID ModelID, systemMessage string) Conversation
}

type conversationalist struct{}

func (c conversationalist) NewConversation(modelID ModelID, systemMessage string) Conversation {
	return NewConversation(modelID, systemMessage)
}

func NewConversationalist() Conversationalist {
	return conversationalist{}
}

type mockConversationalist struct {
	responses map[string]string // responses is a map from keywords in the user message to assistant response

	// NOTE: If we need more fidelity in terms of matching conditions, we can change to, for instance, a slice of structs,
	// where each struct has multiple conditions (ex: keyword, provider, quality) and also the response.
}

func (c mockConversationalist) NewConversation(modelID ModelID, systemMessage string) Conversation {
	return NewMockConversation(modelID, systemMessage, c.responses)
}

func NewMockConversationalist(responses map[string]string) Conversationalist {
	return mockConversationalist{responses: responses}
}
