package modellist

import (
	"encoding/json"
	"fmt"
	"slices"
	"sync"
)

// Type identifies the chat-completions API family a provider implements. Clients can use Type to select request/response shaping logic.
type Type string

// Known API families for chat/completions. Keep in sync with isValidType.
const (
	TypeOpenAI    Type = "openai"    // TypeOpenAI denotes providers that implement an OpenAI-compatible chat/completions API.
	TypeAnthropic Type = "anthropic" // TypeAnthropic denotes providers that implement Anthropic's Messages-style API.
	TypeGemini    Type = "gemini"    // TypeGemini denotes providers that implement Google's Gemini API.
	TypeAzure     Type = "azure"     // TypeAzure denotes Azure OpenAI Service endpoints (OpenAI-like API with Azure-specific auth/URLs).
	TypeBedrock   Type = "bedrock"   // TypeBedrock denotes AWS Bedrock model invocation for conversational models.
	TypeVertexAI  Type = "vertexai"  // TypeVertexAI denotes Google Vertex AI generative models endpoints.
)

// ProviderID represents the inference provider identifier.
type ProviderID string

// Model describes a single model made available by a provider, including pricing, token limits, and capabilities.
type Model struct {
	ID                 string  `json:"id"`                     // ID is the canonical model identifier used in API requests.
	Name               string  `json:"name"`                   // Name is a human-readable model name suitable for display.
	IsLegacy           bool    `json:"is_legacy"`              // IsLegacy reports if this model is considered "legacy" and outdated.
	CostPer1MIn        float64 `json:"cost_per_1m_in"`         // CostPer1MIn is the price per 1M input tokens.
	CostPer1MOut       float64 `json:"cost_per_1m_out"`        // CostPer1MOut is the price per 1M output tokens.
	CostPer1MInCached  float64 `json:"cost_per_1m_in_cached"`  // CostPer1MInCached is the price per 1M input tokens when caching applies.
	CostPer1MOutCached float64 `json:"cost_per_1m_out_cached"` // CostPer1MOutCached is the price per 1M output tokens when caching applies.
	ContextWindow      int64   `json:"context_window"`         // ContextWindow is the maximum token capacity supported by the model.
	DefaultMaxTokens   int64   `json:"default_max_tokens"`
	CanReason          bool    `json:"can_reason"`            // CanReason reports whether the model supports reasoning modes/capabilities.
	HasReasoningEffort bool    `json:"has_reasoning_efforts"` // HasReasoningEffort reports whether the API accepts a "reasoning_effort" parameter.
	SupportsImages     bool    `json:"supports_attachments"`  // SupportsImages reports whether the model accepts image inputs.

	// Other fields that are present in the config:
	// DefaultReasoningEffort string  `json:"default_reasoning_effort,omitempty"` // TODO: I am not sure if we want to use this. Who is it default for?
}

func (p Provider) DefaultLargeModel() Model {
	for _, m := range p.Models {
		if m.ID == p.DefaultLargeModelID {
			return m
		}
	}
	return Model{}
}

func (p Provider) DefaultSmallModel() Model {
	for _, m := range p.Models {
		if m.ID == p.DefaultSmallModelID {
			return m
		}
	}
	return Model{}
}

// Provider describes an inference provider, including its identity, API family, endpoints, defaults, and the catalog of models it exposes.
type Provider struct {
	// Name is the human-readable provider name.
	Name string `json:"name"`

	// ID is the stable machine identifier for the provider.
	ID ProviderID `json:"id"`

	// All providers have an API "type", which is the shape of its API for chat completions. Any bespoke API will have its own type. Commonly new providers will ship APIs that are of the
	// same shape as, for instance, OpenAI's API, so that people can plug-and-play without building to a new API.
	Type Type `json:"type"`

	APIEndpointURL      string  `json:"api_endpoint_url"`                 // URL of their API endpoint.
	APIKeyEnv           string  `json:"api_key,omitempty"`                // APIKey is the env variable that is typically used for auth.
	APIEndpointEnv      string  `json:"api_endpoint_env,omitempty"`       // APIEndpointEnv is the env variable that is recommended to be overridden to use a custom URL.
	DefaultLargeModelID string  `json:"default_large_model_id,omitempty"` // DefaultLargeModelID is the "best" large model of the provider. ID must match id in Models.
	DefaultSmallModelID string  `json:"default_small_model_id,omitempty"` // DefaultSmallModelID is the "best" small model of the provider. ID must match id in Models.
	Models              []Model `json:"models,omitempty"`                 // Models lists the models exposed by this provider.
}

// Synchronization and cache for provider loading.
var (
	getProvidersMutex sync.RWMutex // getProvidersMutex guards cachedProviders and GetProviders' initialization path.
	cachedProviders   []Provider   // cachedProviders stores the memoized providers; non-nil once populated and accessed under getProvidersMutex.
)

// GetProviders returns providers. It is thread-safe, but the slice it returns MUST NOT be modified.
func GetProviders() []Provider {
	getProvidersMutex.RLock()
	if cachedProviders != nil {
		defer getProvidersMutex.RUnlock()
		return cachedProviders
	}
	getProvidersMutex.RUnlock()

	getProvidersMutex.Lock()
	defer getProvidersMutex.Unlock()

	if cachedProviders != nil {
		return cachedProviders
	}

	var out []Provider
	for _, c := range configs {
		if len(c.rawBytes) == 0 {
			panic(fmt.Errorf("empty config for %s", c.name))
		}

		var p Provider
		if err := json.Unmarshal(c.rawBytes, &p); err != nil {
			panic(err)
		}

		if err := checkInvariants(p); err != nil {
			panic(err)
		}

		out = append(out, p)
	}

	cachedProviders = out
	return cachedProviders
}

// checkInvariants validates a Provider and returns an error describing the first violation. It enforces the following rules:
//   - Name and ID must be non-empty.
//   - APIEndpointURL must be non-empty unless the provider ID is one of {"azure","vertexai","bedrock"}.
//   - Type must be one of the supported Type values (see isValidType).
//   - DefaultLargeModelID and DefaultSmallModelID, if set, must exist in Models.
func checkInvariants(p Provider) error {
	if p.Name == "" {
		return fmt.Errorf("provider has empty name (id=%q)", p.ID)
	}
	if p.ID == "" {
		return fmt.Errorf("provider %q has empty id", p.Name)
	}
	apiEndpointBlankAllowed := []ProviderID{"azure", "vertexai", "bedrock"}

	if !slices.Contains(apiEndpointBlankAllowed, p.ID) {
		if p.APIEndpointURL == "" {
			return fmt.Errorf("provider %q has empty api_endpoint_url", p.ID)
		}
	}

	if !IsValidType(p.Type) {
		return fmt.Errorf("provider %q has invalid type %q", p.ID, p.Type)
	}

	modelIDs := make(map[string]struct{}, len(p.Models))
	for _, m := range p.Models {
		if m.ID != "" {
			modelIDs[m.ID] = struct{}{}
		}
	}
	if p.DefaultLargeModelID != "" {
		if _, ok := modelIDs[p.DefaultLargeModelID]; !ok {
			return fmt.Errorf("provider %q default_large_model_id %q not found in models", p.ID, p.DefaultLargeModelID)
		}
	}
	if p.DefaultSmallModelID != "" {
		if _, ok := modelIDs[p.DefaultSmallModelID]; !ok {
			return fmt.Errorf("provider %q default_small_model_id %q not found in models", p.ID, p.DefaultSmallModelID)
		}
	}
	return nil
}

// IsValidType reports whether t is one of the supported API families defined by the Type constants.
func IsValidType(t Type) bool {
	switch t {
	case TypeOpenAI, TypeAnthropic, TypeGemini, TypeAzure, TypeBedrock, TypeVertexAI:
		return true
	default:
		return false
	}
}
