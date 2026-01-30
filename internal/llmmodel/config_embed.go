package llmmodel

import (
	_ "embed"
)

// Embedded provider configuration documents. Keep the list sorted by provider identifier for readability.

//go:embed config/openai.json
var openAIConfig []byte

//go:embed config/anthropic.json
var anthropicConfig []byte

//go:embed config/gemini.json
var geminiConfig []byte

//go:embed config/xai.json
var xaiConfig []byte

var embeddedProviderConfigs = map[ProviderID][]byte{
	ProviderIDOpenAI:    openAIConfig,
	ProviderIDAnthropic: anthropicConfig,
	ProviderIDGemini:    geminiConfig,
	ProviderIDXAI:       xaiConfig,
}
