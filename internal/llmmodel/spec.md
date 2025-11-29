# llmmodel

llmmodel is a package that exposes data related to LLM models and providers. It can answer questions like:
- What are the available models and providers in our "database"?
- For each model, what is the provider?
- Given a model (ex: "gpt-5-codex" from OpenAI), what are its:
    - Context sizes
    - Costs per token
    - URL
    - Environment variables to get API keys

Further, llmmodel allows creation of "custom" models. These models might be like "gpt-5-codex but with high reasoning", or local inference with custom URLs.

Other notes:
- Env var strings can have an optional leading "$" (ex: "$ANTHROPIC_API_KEY"). Any access of the ENV always strips the leading "$".

## Usage

By default, this package will have the set of top models/providers "loaded".

Consumers can then configure those:
- Call AvailableModelIDs() and GetModelInfo() to get default loaded state.
- Call ConfigureProviderKey to configure API keys (if, for instance, they have a config file that end-users save their keys in).
- Call AddCustomModel() to add more models. They might do this for reasons:
    - Add custom params on a per-model basis (ex: reasoning effort)
    - Create custom ModelID aliases
    - Add custom providers. For instance, local inference can be used by setting APIEndpointURL and (APIActualKey or APIEnvKey). ProviderID would be an API-compatible provider (likely OpenAI).

Once configured, params of type ModelID can be passed around to select a model. A package that uses llmmodel to send API requests can accept this ModelID param, get the API key, and get relevant parameters (URL, ReasoningEffort overrides, etc).

## Implementation details

The config/ folder contains a per-provider .JSON file with relevant data. Use go:embed to embed each file into an unexported package-level var.

Each config file has this shape:

```json
{
  "id": "openai",   // ProviderID
  "types": ["openai_responses", "openai_completions"], // Slice of ProviderAPIType values the provider implements.
  "api_endpoint_url": "https://api.openai.com/v1",
  "api_key": "$OPENAI_API_KEY",
  "default_model_id": "gpt-5",
  "models": [
    {
      "id": "gpt-5", // ProviderModelID
      "cost_per_1m_in": 1.25,
      "cost_per_1m_out": 10,
      "cost_per_1m_in_cached": 0.25,
      "context_window": 400000,
      "max_output": 128000,
      "can_reason": true,
      "has_reasoning_effort": true,
      "supports_images": true,
      "is_legacy": false
    },
    ...
  ]
}
```

Upon init, each file will be parsed. But only the files from {openai, anthropic, gemini, xai} will be used in populating a package variable containing []ModelInfo -- the "database" of our active models. Only non is_legacy models will be added from these providers during init.

During this process, we'll need to determine the ModelID for each model. ModelID must be unique across all models from all providers. We must ensure good, clean names for our primary providers' models. For instance, Anthropic's date-based suffixes must be stripped.

However, we can't simply throw away data from other providers and legacy models. These may be referred to when calling AddCustomModel.

## Public Interface

```go
// ModelID is a user-visible ID for a model from the perspective of consumers of this package. It is NOT (necessarily) the same as the model ID sent to API endpoints.
// Consumers can create/register their own ModelID with AddCustomModel, which bundles a provider model as well as a set of parameters.
// This also lets this package and consumers alias long/awkward ids with nicer ones (ex: "claude-sonnet-4-5" vs "claude-sonnet-4-5-20250929").
type ModelID string

// ProviderID returns id's provider.
func (id ModelID) ProviderID() ProviderID

// Valid returns true if it is a known and valid model ID.
func (id ModelID) Valid() bool

// ModelIDUnknown is an unknown model ID (which is also the zero value).
//
// NOTE: I don't want to have ModelIDXyz constants for all our models, because I want them to be more dynamic. I don't want to keep changing them
// every time a model is added or deprecated. These things move fast.
const ModelIDUnknown ModelID = ""

type ModelOverrides struct {
	APIActualKey    string // ex: "123-456"
	APIEnvKey       string // ex: "$ANTHROPIC_API_KEY" or "ANTHROPIC_API_KEY"
	APIEndpointURL  string // ex: "https://api.anthropic.com" or "https://api.openai.com/v1"
	ReasoningEffort string // ex: "medium"
}

type ProviderID string

// DefaultModel returns the default model ID for pid.
func (pid ProviderID) DefaultModel() ModelID

// ProviderAPIType identifies one API "shape" a provider supports. Providers can expose multiple API types simultaneously (ex: OpenAI exposes both Responses and Completions).
type ProviderAPIType string

// Known API families for chats/completions/responses.
const (
	ProviderTypeUnknown           ProviderAPIType = ""
	ProviderTypeOpenAIResponses   ProviderAPIType = "openai_responses"
	ProviderTypeOpenAICompletions ProviderAPIType = "openai_completions"
	ProviderTypeAnthropic         ProviderAPIType = "anthropic"
	ProviderTypeGemini            ProviderAPIType = "gemini"
)

// Valid returns true if pt is known and valid.
func (pt ProviderAPIType) Valid() bool

// Constants for provider IDs. We WILL have each provider have its own constant, unlike models, because we often need to actually add code to support a provider and its API.
const (
	ProviderIDUnknown     ProviderID = ""
	ProviderIDOpenAI      ProviderID = "openai"
	ProviderIDAnthropic   ProviderID = "anthropic"
	ProviderIDGemini      ProviderID = "gemini"
	ProviderIDXAI         ProviderID = "xai"
	ProviderIDOpenRouter  ProviderID = "openrouter"
	ProviderIDHuggingFace ProviderID = "huggingface"
	ProviderIDDeepseek    ProviderID = "deepseek"
	ProviderIDGroq        ProviderID = "groq"
	ProviderIDZAI         ProviderID = "zai"
)

// AllProviderIDs are all provider ids. They are sorted by my personal opinion of importance.
var AllProviderIDs = []ProviderID{
	ProviderIDOpenAI,
	ProviderIDXAI,
	ProviderIDAnthropic,
	ProviderIDGemini,
	ProviderIDOpenRouter,
	ProviderIDHuggingFace,
	ProviderIDDeepseek,
	ProviderIDGroq,
	ProviderIDZAI,
}

// AddCustomModel adds the custom model to the available models. id is an opaque identifier that can be referred to later from consumers of this package.
// providerID must match a known provider (note: for truly custom models, just say it's openai, or whatever shape the API is, and use custom URL in overrides).
// providerModelID is the API parameter sent to the LLM provider for 'model' - matches API provider docs (ex: "claude-opus-4-20250514").
//
// It returns an error if:
//   - invalid id/providerID
//   - duplicate id
//
// However, as long as the ID is unique, it can duplicate model/parameters.
func AddCustomModel(id ModelID, providerID ProviderID, providerModelID string, overrides ModelOverrides) error

// AvailableModelIDs returns the list of user-visible model IDs registered with llmmodel.
func AvailableModelIDs() []ModelID

// ModelIDOrFallback returns id if it is valid. Otherwise it returns the default
// model for ProviderIDOpenAI (or the first available model, if ProviderIDOpenAI has no models).
//
// This method can be used in cases where the consumer must talk to *some* valid model, but their current model id might be unset or invalid.
func ModelIDOrFallback(id ModelID) ModelID

type ModelInfo struct {
	ID              ModelID
	ProviderID      ProviderID
	SupportedTypes  []ProviderAPIType
	ProviderModelID string // the model identifier used in API requests.
	IsDefault       bool

	APIEndpointURL string

	// Note on pricing: uniformly modeling pricing across all providers is fraught. These numbers serve as rough guidelines. Some providers might be modeled very poorly.
	// As of 2025/10/23:
	//   - Gemini has tiered CostPer1MInCached rates by token count (cost increases for tokens past 200k)
	//   - Anthropic has a cost to write to cache, based on cache TTL. They also require developers specifically insert cache commands into API requests to use it.

	CostPer1MIn            float64 // CostPer1MIn is the price per 1M input tokens.
	CostPer1MOut           float64 // CostPer1MOut is the price per 1M output tokens.
	CostPer1MInCached      float64 // CostPer1MInCached is the price per 1M input tokens when caching applies.
	CostPer1MInSaveToCache float64 // Cost to SAVE 1M tokens to cache. As of 2025-10-22, applies only to Anthropic.

	ContextWindow int64 // ContextWindow is the maximum token capacity supported by the model.

	MaxOutput int64 // MaxOutput is the max number of output tokens the model can generate per request.

	CanReason          bool // CanReason reports whether the model supports reasoning modes/capabilities.
	HasReasoningEffort bool // HasReasoningEffort reports whether the API accepts a "reasoning_effort" parameter (or similar).
	SupportsImages     bool // SupportsImages reports whether the model accepts image inputs.

	ModelOverrides
}

// GetModelInfo returns information for the corresponding model ID.
func GetModelInfo(id ModelID) ModelInfo

// ConfigureProviderKey configures the provider to use the provided API key.
func ConfigureProviderKey(providerID ProviderID, key string)

// EnvHasDefaultKey returns true if the current env has a value for the provider's default key.
// Example: EnvHasDefaultKey(ProviderIDOpenAI) checks if "OPENAI_API_KEY" is present and non-blank in ENV.
// Note that this ONLY checks defaults, not any *overridden* env key.
func EnvHasDefaultKey(providerID ProviderID) bool

// ProviderKeyEnvVars returns a map of provider id to default env var (without $) for all providers in AllProviderIDs.
// Ex: {ProviderIDOpenAI: "OPENAI_API_KEY", ...}
func ProviderKeyEnvVars() map[ProviderID]string

// GetAPIKey returns the API key for the model with id ("" if not found). This is the precedence:
//  1. ModelInfo.ModelOverrides.APIActualKey
//  2. Env[ModelInfo.ModelOverrides.APIEnvKey]
//  3. Value from ConfigureProviderKey for id.ProviderID()
//  4. Env[ProviderKeyEnvVars()[id.ProviderID()]]
func GetAPIKey(id ModelID) string

// GetAPIEndpointURL returns the API endpoint URL for the model with id ("" if not found). This is the precedence:
//  1. ModelInfo.ModelOverrides.APIEndpointURL
//  2. ModelInfo.APIEndpointURL
func GetAPIEndpointURL(id ModelID) string

```
