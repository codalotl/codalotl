package modellist

import (
	_ "embed"
)

//go:embed config/openai.json
var openAIConfig []byte

//go:embed config/xai.json
var xaiConfig []byte

//go:embed config/groq.json
var groqConfig []byte

//go:embed config/anthropic.json
var anthropicConfig []byte

//go:embed config/aihubmix.json
var aihubmixConfig []byte

//go:embed config/azure.json
var azureConfig []byte

//go:embed config/bedrock.json
var bedrockConfig []byte

//go:embed config/cerebras.json
var cerebrasConfig []byte

//go:embed config/chutes.json
var chutesConfig []byte

//go:embed config/deepseek.json
var deepseekConfig []byte

//go:embed config/gemini.json
var geminiConfig []byte

//go:embed config/huggingface.json
var huggingfaceConfig []byte

//go:embed config/openrouter.json
var openrouterConfig []byte

//go:embed config/venice.json
var veniceConfig []byte

//go:embed config/vertexai.json
var vertexaiConfig []byte

//go:embed config/zai.json
var zaiConfig []byte

// configProvider holds an embedded provider configuration and its identifier. It is internal to this package and used to construct Provider values at runtime.
type configProvider struct {
	rawBytes []byte // rawBytes is the embedded JSON document for a single Provider.
	name     string // name is the provider identifier (ex: "openai") used for listing and error reporting.
}

// configs contains all embedded provider configurations in the order they are exposed. The order is stable and defines the iteration order for GetProviders and the names returned by
// GetProviderNames.
var configs = []configProvider{
	{rawBytes: openAIConfig, name: "openai"},
	{rawBytes: xaiConfig, name: "xai"},
	{rawBytes: groqConfig, name: "groq"},
	{rawBytes: anthropicConfig, name: "anthropic"},
	{rawBytes: aihubmixConfig, name: "aihubmix"},
	{rawBytes: azureConfig, name: "azure"},
	{rawBytes: bedrockConfig, name: "bedrock"},
	{rawBytes: cerebrasConfig, name: "cerebras"},
	{rawBytes: chutesConfig, name: "chutes"},
	{rawBytes: deepseekConfig, name: "deepseek"},
	{rawBytes: geminiConfig, name: "gemini"},
	{rawBytes: huggingfaceConfig, name: "huggingface"},
	{rawBytes: openrouterConfig, name: "openrouter"},
	{rawBytes: veniceConfig, name: "venice"},
	{rawBytes: vertexaiConfig, name: "vertexai"},
	{rawBytes: zaiConfig, name: "zai"},
}

// GetProviderNames returns the identifiers of all embedded providers in a stable order. The order matches the internal configs list. The returned slice is a fresh allocation when non-empty;
// it is nil if no providers are configured.
func GetProviderNames() []string {
	var out []string

	for _, c := range configs {
		out = append(out, c.name)
	}

	return out
}
