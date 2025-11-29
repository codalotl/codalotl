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

//go:embed config/openrouter.json
var openRouterConfig []byte

//go:embed config/huggingface.json
var huggingFaceConfig []byte

//go:embed config/deepseek.json
var deepSeekConfig []byte

//go:embed config/groq.json
var groqConfig []byte

//go:embed config/zai.json
var zaiConfig []byte

var embeddedProviderConfigs = map[ProviderID][]byte{
	ProviderIDOpenAI:      openAIConfig,
	ProviderIDAnthropic:   anthropicConfig,
	ProviderIDGemini:      geminiConfig,
	ProviderIDXAI:         xaiConfig,
	ProviderIDOpenRouter:  openRouterConfig,
	ProviderIDHuggingFace: huggingFaceConfig,
	ProviderIDDeepseek:    deepSeekConfig,
	ProviderIDGroq:        groqConfig,
	ProviderIDZAI:         zaiConfig,
}
