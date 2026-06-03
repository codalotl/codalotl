package llmmodel

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ModelID is a user-visible ID for a model from the perspective of consumers of this package. It is NOT (necessarily) the same as the model ID sent to API endpoints.
// Consumers can create/register their own ModelID with AddCustomModel, which bundles a provider model as well as a set of parameters. This also lets this package
// and consumers alias long/awkward IDs with nicer ones (ex: "claude-sonnet-4-5" vs "claude-sonnet-4-5-20250929").
type ModelID string

// DefaultModel is a good default model. It can be used in tests or in production code.
//
// Applications probably want to define their own default model.
const DefaultModel ModelID = "gpt-5.5-high"

// ProviderID returns id's provider.
func (id ModelID) ProviderID() ProviderID {
	info := GetModelInfo(id)
	return info.ProviderID
}

// Valid returns true if it is a known and valid model ID.
func (id ModelID) Valid() bool {
	if id == ModelIDUnknown {
		return false
	}
	modelsMu.RLock()
	defer modelsMu.RUnlock()
	_, ok := modelsByID[id]
	return ok
}

// ModelIDUnknown is an unknown model ID (which is also the zero value).
//
// NOTE: I don't want to have ModelIDXyz constants for all our models, because I want them to be more dynamic. I don't want to keep changing them every time a model
// is added or deprecated. These things move fast.
const ModelIDUnknown ModelID = ""

// ModelOverrides contains optional per-model settings that take precedence over provider defaults where supported.
//
// APIEndpointURL records an explicit per-model endpoint override. It is preserved separately from ModelInfo.APIEndpointURL; use GetAPIEndpointURL to resolve the
// effective endpoint for a model.
type ModelOverrides struct {
	APIActualKey    string // ex: "123-456"
	APIEnvKey       string // ex: "$ANTHROPIC_API_KEY" or "ANTHROPIC_API_KEY"
	APIEndpointURL  string // ex: "https://api.anthropic.com" or "https://api.openai.com/v1"
	ReasoningEffort string // ex: "medium"
	ServiceTier     string // ex: "priority"
}

// ProviderID identifies an LLM provider known to llmmodel.
type ProviderID string

// DefaultModel returns the default model ID for pid.
//
// It returns ModelIDUnknown for an unknown provider ID. Use ModelIDOrFallback when callers need a best-effort fallback to any known model.
func (pid ProviderID) DefaultModel() ModelID {
	modelsMu.RLock()
	defer modelsMu.RUnlock()
	return providerDefaults[pid]
}

// ProviderSubscription is provider-agnostic subscription auth that can be used instead of a provider API key.
//
// Subscription auth is considered usable only when it matches the requested provider, has nonblank AccessToken, AccountID, and APIEndpointURL fields, and has not
// expired. Subscription auth applies at the provider level only for registered models without per-model APIActualKey, a usable APIEnvKey value, or APIEndpointURL
// overrides.
type ProviderSubscription struct {
	ProviderID       ProviderID // ProviderID is the provider this subscription applies to.
	AccessToken      string     // AccessToken is the subscription access token used to authorize provider requests.
	AccountID        string     // AccountID is the provider account identifier associated with the subscription.
	APIEndpointURL   string     // APIEndpointURL is the endpoint used for requests authorized by this subscription.
	ExpiresAt        time.Time  // ExpiresAt is the time after which the subscription must not be used.
	RequiresNoStore  bool       // RequiresNoStore reports whether requests using this subscription must request provider no-store behavior.
	RootInstructions bool       // RootInstructions reports whether requests using this subscription should enable provider-specific root-instruction handling.
}

// SetProviderSubscription configures subscription auth for a provider.
func SetProviderSubscription(providerID ProviderID, sub ProviderSubscription) {
	modelsMu.Lock()
	defer modelsMu.Unlock()
	providerSubscriptions[providerID] = sub
}

// ClearProviderSubscription removes subscription auth for a provider.
func ClearProviderSubscription(providerID ProviderID) {
	modelsMu.Lock()
	defer modelsMu.Unlock()
	delete(providerSubscriptions, providerID)
}

// SetProviderSubscriptionRequired controls whether provider subscription auth is required for providerID.
//
// While required and no usable provider subscription is configured, provider-level API-key fallback is suppressed for models that would otherwise be eligible for
// provider subscription auth. Per-model APIActualKey and usable APIEnvKey overrides still take precedence and are not suppressed.
func SetProviderSubscriptionRequired(providerID ProviderID, required bool) {
	modelsMu.Lock()
	defer modelsMu.Unlock()
	if required {
		providerSubscriptionRequired[providerID] = true
		return
	}
	delete(providerSubscriptionRequired, providerID)
}

// ProviderSubscriptionRequired reports whether provider subscription auth is required for providerID.
func ProviderSubscriptionRequired(providerID ProviderID) bool {
	modelsMu.RLock()
	defer modelsMu.RUnlock()
	return providerSubscriptionRequired[providerID]
}

// GetProviderSubscription returns usable subscription auth for providerID, if set.
//
// Usable subscription auth has matching provider, required fields present, and nonexpired ExpiresAt.
func GetProviderSubscription(providerID ProviderID) (ProviderSubscription, bool) {
	modelsMu.RLock()
	sub, ok := providerSubscriptions[providerID]
	modelsMu.RUnlock()
	if !ok || !usableProviderSubscription(providerID, sub) {
		return ProviderSubscription{}, false
	}
	return sub, true
}

// ProviderHasSubscription reports whether usable subscription auth is configured for providerID.
func ProviderHasSubscription(providerID ProviderID) bool {
	_, ok := GetProviderSubscription(providerID)
	return ok
}

// ModelUsesProviderSubscription reports whether id is currently callable through usable provider subscription auth.
//
// It reports current usable auth, not eligibility in principle: it returns false if no usable subscription is configured. Eligibility is independent of SupportedTypes
// and requires a known model without per-model APIActualKey, a usable APIEnvKey value, or APIEndpointURL overrides.
func ModelUsesProviderSubscription(id ModelID) bool {
	return modelHasEligibleProviderSubscription(id)
}

// ProviderAPIType identifies one API "shape" a provider supports. Providers can expose multiple API types simultaneously (ex: OpenAI exposes both Responses and
// Completions). It does not describe auth availability or provider subscription eligibility.
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
func (pt ProviderAPIType) Valid() bool {
	switch pt {
	case ProviderTypeOpenAIResponses, ProviderTypeOpenAICompletions, ProviderTypeAnthropic, ProviderTypeGemini:
		return true
	default:
		return false
	}
}

// Constants for provider IDs. We WILL have each provider have its own constant, unlike models, because we often need to actually add code to support a provider
// and its API.
const (
	ProviderIDUnknown   ProviderID = ""
	ProviderIDOpenAI    ProviderID = "openai"
	ProviderIDAnthropic ProviderID = "anthropic"
	ProviderIDGemini    ProviderID = "gemini"
	ProviderIDXAI       ProviderID = "xai"
)

// AllProviderIDs are all provider IDs. They are sorted by my personal opinion of importance.
var AllProviderIDs = []ProviderID{
	ProviderIDOpenAI,
	ProviderIDXAI,
	ProviderIDAnthropic,
	ProviderIDGemini,
}

// AddCustomModel adds the custom model to the available models. id is an opaque identifier that can be referred to later from consumers of this package. providerID
// must match a known provider (note: for truly custom models, just say it's openai, or whatever shape the API is, and use custom URL in overrides). providerModelID
// is the API parameter sent to the LLM provider for 'model' - matches API provider docs (ex: "claude-opus-4-20250514").
//
// It returns an error if:
//   - invalid id/providerID
//   - duplicate id
//
// However, as long as the ID is unique, it can duplicate model/parameters.
func AddCustomModel(id ModelID, providerID ProviderID, providerModelID string, overrides ModelOverrides) error {
	if id == ModelIDUnknown {
		return fmt.Errorf("custom model id must not be empty")
	}
	if providerID == ProviderIDUnknown {
		return fmt.Errorf("provider id must not be empty")
	}
	modelsMu.Lock()
	defer modelsMu.Unlock()

	if _, exists := modelsByID[id]; exists {
		return fmt.Errorf("model id %q already registered", id)
	}

	provider, ok := providerCatalog[providerID]
	if !ok {
		return fmt.Errorf("provider %q not recognized", providerID)
	}

	info := ModelInfo{
		ID:              id,
		ProviderID:      providerID,
		SupportedTypes:  append([]ProviderAPIType(nil), provider.SupportedTypes...),
		ProviderModelID: providerModelID,
		APIEndpointURL:  provider.APIEndpointURL,
		ModelOverrides: ModelOverrides{
			APIActualKey:    overrides.APIActualKey,
			APIEnvKey:       normalizeEnvKey(overrides.APIEnvKey),
			APIEndpointURL:  overrides.APIEndpointURL,
			ReasoningEffort: overrides.ReasoningEffort,
			ServiceTier:     overrides.ServiceTier,
		},
	}

	if base, ok := provider.ModelByID[providerModelID]; ok {
		info.CostPer1MIn = base.CostPer1MIn
		info.CostPer1MOut = base.CostPer1MOut
		info.CostPer1MInCached = base.CostPer1MInCached
		info.CostPer1MInSaveToCache = base.CostPer1MInSaveToCache
		info.ContextWindow = base.ContextWindow
		info.MaxOutput = base.MaxOutput
		info.CanReason = base.CanReason
		info.HasReasoningEffort = base.HasReasoningEffort
		info.SupportsAutocompaction = base.SupportsAutocompaction
		info.SupportsImages = base.SupportsImages
	}

	modelsByID[id] = info
	modelOrder = append(modelOrder, id)

	return nil
}

// AvailableModelIDs returns the list of user-visible model IDs registered with llmmodel.
func AvailableModelIDs() []ModelID {
	modelsMu.RLock()
	defer modelsMu.RUnlock()

	out := make([]ModelID, len(modelOrder))
	copy(out, modelOrder)
	return out
}

// ModelIDOrFallback returns id if it is valid. Otherwise it returns the default model for ProviderIDOpenAI (or the first available model, if ProviderIDOpenAI has
// no models).
//
// This method can be used in cases where the consumer must talk to *some* valid model, but their current model ID might be unset or invalid.
func ModelIDOrFallback(id ModelID) ModelID {
	if id.Valid() {
		return id
	}
	modelsMu.RLock()
	defer modelsMu.RUnlock()

	if fallback := providerDefaults[ProviderIDOpenAI]; fallback != ModelIDUnknown {
		return fallback
	}
	if len(modelOrder) > 0 {
		return modelOrder[0]
	}
	return ModelIDUnknown
}

// ModelInfo describes a registered model and its provider metadata.
type ModelInfo struct {
	ID              ModelID           // ID is the user-visible model identifier used by llmmodel consumers.
	ProviderID      ProviderID        // ProviderID identifies the provider that serves the model.
	SupportedTypes  []ProviderAPIType // SupportedTypes lists the provider API shapes supported for the model.
	ProviderModelID string            // the model identifier used in API requests.
	IsDefault       bool              // IsDefault reports whether this model is the default registered model for its provider.
	APIEndpointURL  string            // APIEndpointURL is the provider/default endpoint; per-model overrides remain in ModelOverrides.APIEndpointURL.

	// Note on pricing: uniformly modeling pricing across all providers is fraught. These numbers serve as rough guidelines. Some providers might be modeled very poorly.
	// Some providers have pricing tiers that this flat schema cannot represent:
	//   - OpenAI GPT-5 prompt pricing can increase for very large contexts; record the short-context prompt rates here.
	//   - Gemini has tiered CostPer1MInCached rates by token count (cost increases for tokens past 200k)
	//   - Anthropic has a cost to write to cache, based on cache TTL. They also require developers specifically insert cache commands into API requests to use it.

	CostPer1MIn            float64 // CostPer1MIn is the price per 1M input tokens.
	CostPer1MOut           float64 // CostPer1MOut is the price per 1M output tokens.
	CostPer1MInCached      float64 // CostPer1MInCached is the price per 1M input tokens when caching applies.
	CostPer1MInSaveToCache float64 // Cost to SAVE 1M tokens to cache. As of 2025-10-22, applies only to Anthropic.
	ContextWindow          int64   // ContextWindow is the maximum token capacity supported by the model.
	MaxOutput              int64   // MaxOutput is the max number of output tokens the model can generate per request.
	CanReason              bool    // CanReason reports whether the model supports reasoning modes/capabilities.
	HasReasoningEffort     bool    // HasReasoningEffort reports whether the API accepts a "reasoning_effort" parameter (or similar).
	SupportsAutocompaction bool    // SupportsAutocompaction reports whether the model supports provider-side context autocompaction.
	SupportsImages         bool    // SupportsImages reports whether the model accepts image inputs.
	ModelOverrides                 // ModelOverrides contains explicit per-model settings that override provider defaults where supported.
}

// GetModelInfo returns information for the corresponding model ID.
//
// The returned ModelInfo preserves explicit per-model settings in the embedded ModelOverrides. Use helpers such as GetAPIEndpointURL when callers need resolved
// effective values.
func GetModelInfo(id ModelID) ModelInfo {
	if id == ModelIDUnknown {
		return ModelInfo{}
	}
	modelsMu.RLock()
	defer modelsMu.RUnlock()

	info, ok := modelsByID[id]
	if !ok {
		return ModelInfo{}
	}
	return info
}

// ConfigureProviderKey configures the provider to use the provided API key.
func ConfigureProviderKey(providerID ProviderID, key string) {
	modelsMu.Lock()
	defer modelsMu.Unlock()
	if key == "" {
		delete(providerKeyOverrides, providerID)
		return
	}
	providerKeyOverrides[providerID] = key
}

// EnvHasDefaultKey returns true if the current env has a value for the provider's default key. Example: EnvHasDefaultKey(ProviderIDOpenAI) checks if "OPENAI_API_KEY"
// is present and non-blank in ENV. Note that this ONLY checks defaults, not any *overridden* env key.
func EnvHasDefaultKey(providerID ProviderID) bool {
	env := providerEnvVars[providerID]
	if env == "" {
		return false
	}
	val, ok := os.LookupEnv(env)
	if !ok {
		return false
	}
	return val != ""
}

// ProviderKeyEnvVars returns a map of provider id to default env var (without $) for all providers in AllProviderIDs. Ex: {ProviderIDOpenAI: "OPENAI_API_KEY", ...}
func ProviderKeyEnvVars() map[ProviderID]string {
	out := make(map[ProviderID]string, len(providerEnvVars))
	for pid, env := range providerEnvVars {
		out[pid] = env
	}
	return out
}

// ProviderHasConfiguredKey reports whether a key is configured for providerID via either:
//   - ConfigureProviderKey(providerID, key) (in-memory override), or
//   - the provider's default env var (ex: "OPENAI_API_KEY")
//
// If you need to consider per-model overrides (APIActualKey / APIEnvKey), filter at the model level using GetAPIKey(modelID) instead.
func ProviderHasConfiguredKey(providerID ProviderID) bool {
	if providerID == ProviderIDUnknown {
		return false
	}

	modelsMu.RLock()
	override := providerKeyOverrides[providerID]
	env := providerEnvVars[providerID]
	modelsMu.RUnlock()

	if override != "" {
		return true
	}
	if env == "" {
		return false
	}
	return os.Getenv(env) != ""
}

// GetAPIKey returns the effective API key for the model with id ("" if not found or provider-level fallback is suppressed). This is the precedence:
//  1. ModelInfo.ModelOverrides.APIActualKey
//  2. Env[ModelInfo.ModelOverrides.APIEnvKey]
//  3. Value from ConfigureProviderKey for id.ProviderID()
//  4. Env[ProviderKeyEnvVars()[id.ProviderID()]]
//
// If ProviderSubscriptionRequired is true for the model's provider and no usable subscription is configured, steps 3 and 4 are suppressed for models eligible for
// provider subscription auth. Per-model overrides in steps 1 and 2 are still honored.
func GetAPIKey(id ModelID) string {
	info := GetModelInfo(id)
	if info.ID == ModelIDUnknown {
		return ""
	}
	if info.APIActualKey != "" {
		return info.APIActualKey
	}
	if envKey := info.APIEnvKey; envKey != "" {
		if val := os.Getenv(envKey); val != "" {
			return val
		}
	}
	if providerKeyFallbackSuppressed(info) {
		return ""
	}

	modelsMu.RLock()
	override := providerKeyOverrides[info.ProviderID]
	modelsMu.RUnlock()
	if override != "" {
		return override
	}
	if env := providerEnvVars[info.ProviderID]; env != "" {
		return os.Getenv(env)
	}
	return ""
}

// AvailableModelIDsWithAPIKey returns only the model IDs that currently have a non-empty effective API key (per GetAPIKey).
func AvailableModelIDsWithAPIKey() []ModelID {
	ids := AvailableModelIDs()
	out := make([]ModelID, 0, len(ids))
	for _, id := range ids {
		if GetAPIKey(id) != "" {
			out = append(out, id)
		}
	}
	return out
}

// AvailableModelIDsWithAuth returns only the model IDs that currently have a non-empty effective API key or currently usable provider subscription auth.
func AvailableModelIDsWithAuth() []ModelID {
	ids := AvailableModelIDs()
	out := make([]ModelID, 0, len(ids))
	for _, id := range ids {
		if GetAPIKey(id) != "" || modelHasEligibleProviderSubscription(id) {
			out = append(out, id)
		}
	}
	return out
}

// GetAPIEndpointURL returns the API endpoint URL for the model with id ("" if not found). This is the precedence:
//  1. ModelInfo.ModelOverrides.APIEndpointURL
//  2. ModelInfo.APIEndpointURL
func GetAPIEndpointURL(id ModelID) string {
	info := GetModelInfo(id)
	if info.ID == ModelIDUnknown {
		return ""
	}
	if info.ModelOverrides.APIEndpointURL != "" {
		return info.ModelOverrides.APIEndpointURL
	}
	return info.APIEndpointURL
}

// internal structures and initialization.

// providerConfigFile is the top-level schema for an embedded provider JSON config.
type providerConfigFile struct {
	ID             string                 `json:"id"`               // ID is the provider identifier declared by the config.
	Types          []string               `json:"types"`            // Types lists the provider API types declared by the config.
	APIEndpointURL string                 `json:"api_endpoint_url"` // APIEndpointURL is the provider's default API endpoint.
	APIKey         string                 `json:"api_key"`          // APIKey is the provider's default API key environment variable, optionally prefixed with "$".
	DefaultModelID string                 `json:"default_model_id"` // DefaultModelID is the provider-side model ID to use as the provider default.
	Models         []providerModelPayload `json:"models"`           // Models lists the provider-side models declared by the config.
}

// providerModelPayload is one model entry from an embedded provider JSON config.
type providerModelPayload struct {
	ID                     string  `json:"id"`                           // ID is the provider-side model identifier sent in API requests.
	CostPer1MIn            float64 `json:"cost_per_1m_in"`               // CostPer1MIn is the price per 1M input tokens.
	CostPer1MOut           float64 `json:"cost_per_1m_out"`              // CostPer1MOut is the price per 1M output tokens.
	CostPer1MInCached      float64 `json:"cost_per_1m_in_cached"`        // CostPer1MInCached is the price per 1M input tokens when cache-read pricing applies.
	CostPer1MOutCached     float64 `json:"cost_per_1m_out_cached"`       // CostPer1MOutCached is the price per 1M output tokens when cached-output pricing applies.
	CostPer1MInSaveToCache float64 `json:"cost_per_1m_in_save_to_cache"` // CostPer1MInSaveToCache is the price to write 1M input tokens to a provider cache.
	ContextWindow          int64   `json:"context_window"`               // ContextWindow is the maximum token capacity supported by the model.
	MaxOutput              int64   `json:"max_output"`                   // MaxOutput is the maximum number of output tokens the model can generate per request.
	CanReason              bool    `json:"can_reason"`                   // CanReason reports whether the model supports reasoning capabilities.

	// HasReasoningEffort reports whether the provider API accepts a reasoning-effort parameter for the model.
	HasReasoningEffort bool `json:"has_reasoning_effort"`

	// SupportsAutocompaction reports whether the model supports provider-side context autocompaction.
	SupportsAutocompaction bool `json:"supports_autocompaction"`

	// SupportsImages reports whether the model accepts image inputs.
	SupportsImages bool `json:"supports_images"`

	// IsLegacy reports whether the model should remain in the catalog but be excluded from default registration.
	IsLegacy bool `json:"is_legacy"`
}

// providerData is the normalized in-memory representation of a provider config.
type providerData struct {
	ID                   ProviderID                      // ID is the provider identifier.
	SupportedTypes       []ProviderAPIType               // SupportedTypes lists the validated API types supported by the provider.
	APIEndpointURL       string                          // APIEndpointURL is the provider's default API endpoint.
	DefaultProviderModel string                          // DefaultProviderModel is the provider-side model ID declared as the provider default.
	APIKeyEnv            string                          // APIKeyEnv is the provider's default API key environment variable without a leading "$".
	Models               []providerModelPayload          // Models lists all provider-side model records loaded from the config.
	ModelByID            map[string]providerModelPayload // ModelByID indexes Models by provider-side model identifier.
}

// Package registry state stores registered models, provider configuration, API-key overrides, and subscription auth.
var (
	modelsMu             sync.RWMutex                   // modelsMu protects the mutable package registries in this block.
	modelsByID           = make(map[ModelID]ModelInfo)  // modelsByID maps registered user-visible model IDs to model metadata.
	modelOrder           []ModelID                      // modelOrder preserves model registration order for AvailableModelIDs and fallback selection.
	providerDefaults     = make(map[ProviderID]ModelID) // providerDefaults maps each provider to its default user-visible model ID.
	providerEnvVars      = make(map[ProviderID]string)  // providerEnvVars maps each provider to its default API key environment variable without a leading "$".
	providerKeyOverrides = make(map[ProviderID]string)  // providerKeyOverrides stores in-memory provider API keys configured with ConfigureProviderKey.

	// providerCatalog stores normalized provider configs and all provider-side models loaded from embedded config files.
	providerCatalog = make(map[ProviderID]providerData)

	// providerSubscriptions stores provider-level subscription auth configured with SetProviderSubscription.
	providerSubscriptions = make(map[ProviderID]ProviderSubscription)

	// providerSubscriptionRequired tracks providers whose saved subscription auth must be used when subscription auth applies.
	providerSubscriptionRequired = make(map[ProviderID]bool)
)

var anthropicVersionSuffix = regexp.MustCompile(`-\d{6,}$`)
var dashBetweenDigits = regexp.MustCompile(`(\d)-(\d)`)

func init() {
	if err := loadProviders(); err != nil {
		panic(err)
	}
	registerPrimaryModels()
}

// loadProviders parses and validates embedded provider configs into the provider catalog.
//
// It loads providers listed in AllProviderIDs, normalizes API key environment variables, builds provider model indexes, and records default provider env vars.
func loadProviders() error {
	for _, pid := range AllProviderIDs {
		raw, ok := embeddedProviderConfigs[pid]
		if !ok {
			return fmt.Errorf("missing embedded config for provider %q", pid)
		}
		if len(raw) == 0 {
			return fmt.Errorf("empty embedded config for provider %q", pid)
		}

		var cfg providerConfigFile
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return fmt.Errorf("provider %q config invalid: %w", pid, err)
		}
		if cfg.ID == "" {
			return fmt.Errorf("provider %q has blank id", pid)
		}
		if ProviderID(cfg.ID) != pid {
			return fmt.Errorf("provider id mismatch: expected %q got %q", pid, cfg.ID)
		}

		if len(cfg.Types) == 0 {
			return fmt.Errorf("provider %q has no types", pid)
		}

		supported := make([]ProviderAPIType, 0, len(cfg.Types))
		for _, rawType := range cfg.Types {
			pt := ProviderAPIType(rawType)
			if !pt.Valid() {
				return fmt.Errorf("provider %q has unsupported type %q", pid, rawType)
			}
			supported = append(supported, pt)
		}

		envKey := normalizeEnvKey(cfg.APIKey)

		modelByID := make(map[string]providerModelPayload, len(cfg.Models))
		for _, m := range cfg.Models {
			modelByID[m.ID] = m
		}

		providerCatalog[pid] = providerData{
			ID:                   pid,
			SupportedTypes:       supported,
			APIEndpointURL:       cfg.APIEndpointURL,
			DefaultProviderModel: cfg.DefaultModelID,
			APIKeyEnv:            envKey,
			Models:               cfg.Models,
			ModelByID:            modelByID,
		}
		providerEnvVars[pid] = envKey
	}
	return nil
}

// registerPrimaryModels registers the built-in user-visible models from the provider catalog.
//
// It skips legacy models, derives globally unique model IDs, adds required OpenAI reasoning variants, and fills provider defaults.
func registerPrimaryModels() {
	modelsMu.Lock()
	defer modelsMu.Unlock()

	type reasoningVariant struct {
		suffix string
		effort string
	}
	reasoningVariants := []reasoningVariant{
		{suffix: "medium", effort: "medium"},
		{suffix: "high", effort: "high"},
		{suffix: "xhigh", effort: "xhigh"},
	}

	registerOpenAIReasoningVariants := func(provider providerData, m providerModelPayload, firstModel *ModelID) {
		for _, variant := range reasoningVariants {
			candidate := ModelID(fmt.Sprintf("%s-%s", m.ID, variant.suffix))
			unique := ensureUniqueModelIDLocked(provider.ID, candidate, m.ID)
			if *firstModel == ModelIDUnknown {
				*firstModel = unique
			}

			info := ModelInfo{
				ID:                     unique,
				ProviderID:             provider.ID,
				SupportedTypes:         []ProviderAPIType{ProviderTypeOpenAIResponses},
				ProviderModelID:        m.ID,
				IsDefault:              m.ID == provider.DefaultProviderModel && variant.suffix == "high",
				APIEndpointURL:         provider.APIEndpointURL,
				CostPer1MIn:            m.CostPer1MIn,
				CostPer1MOut:           m.CostPer1MOut,
				CostPer1MInCached:      m.CostPer1MInCached,
				CostPer1MInSaveToCache: m.CostPer1MInSaveToCache,
				ContextWindow:          m.ContextWindow,
				MaxOutput:              m.MaxOutput,
				CanReason:              m.CanReason,
				HasReasoningEffort:     m.HasReasoningEffort,
				SupportsAutocompaction: m.SupportsAutocompaction,
				SupportsImages:         m.SupportsImages,
				ModelOverrides: ModelOverrides{
					ReasoningEffort: variant.effort,
				},
			}

			modelsByID[unique] = info
			modelOrder = append(modelOrder, unique)

			if info.IsDefault && providerDefaults[provider.ID] == ModelIDUnknown {
				providerDefaults[provider.ID] = unique
			}
		}
	}

	primary := []ProviderID{
		ProviderIDOpenAI,
		ProviderIDAnthropic,
		ProviderIDGemini,
		ProviderIDXAI,
	}

	for _, pid := range primary {
		provider := providerCatalog[pid]
		firstModel := ModelIDUnknown

		for _, m := range provider.Models {
			if m.IsLegacy {
				continue
			}

			if pid == ProviderIDOpenAI && (m.ID == provider.DefaultProviderModel || m.ID == "gpt-5.3-codex") {
				registerOpenAIReasoningVariants(provider, m, &firstModel)
				continue // don't register the base id; force selection of a reasoning variant.
			}

			candidate := deriveModelID(pid, m.ID)
			if candidate == ModelIDUnknown {
				continue
			}

			unique := ensureUniqueModelIDLocked(pid, candidate, m.ID)
			if firstModel == ModelIDUnknown {
				firstModel = unique
			}

			info := ModelInfo{
				ID:                     unique,
				ProviderID:             pid,
				SupportedTypes:         append([]ProviderAPIType(nil), provider.SupportedTypes...),
				ProviderModelID:        m.ID,
				IsDefault:              m.ID == provider.DefaultProviderModel,
				APIEndpointURL:         provider.APIEndpointURL,
				CostPer1MIn:            m.CostPer1MIn,
				CostPer1MOut:           m.CostPer1MOut,
				CostPer1MInCached:      m.CostPer1MInCached,
				CostPer1MInSaveToCache: m.CostPer1MInSaveToCache,
				ContextWindow:          m.ContextWindow,
				MaxOutput:              m.MaxOutput,
				CanReason:              m.CanReason,
				HasReasoningEffort:     m.HasReasoningEffort,
				SupportsAutocompaction: m.SupportsAutocompaction,
				SupportsImages:         m.SupportsImages,
			}

			modelsByID[unique] = info
			modelOrder = append(modelOrder, unique)

			if info.IsDefault && providerDefaults[pid] == ModelIDUnknown {
				providerDefaults[pid] = unique
			}

		}

		if providerDefaults[pid] == ModelIDUnknown {
			providerDefaults[pid] = firstModel
		}
	}
}

func normalizeEnvKey(value string) string {
	if value == "" {
		return ""
	}
	if value[0] == '$' {
		return value[1:]
	}
	return value
}

func usableProviderSubscription(providerID ProviderID, sub ProviderSubscription) bool {
	if providerID == ProviderIDUnknown || sub.ProviderID != providerID {
		return false
	}
	if strings.TrimSpace(sub.AccessToken) == "" ||
		strings.TrimSpace(sub.AccountID) == "" ||
		strings.TrimSpace(sub.APIEndpointURL) == "" {
		return false
	}
	return sub.ExpiresAt.After(time.Now())
}

func modelEligibleForProviderSubscription(info ModelInfo) bool {
	return info.ID != ModelIDUnknown &&
		info.APIActualKey == "" &&
		!modelHasUsableAPIEnvKey(info) &&
		info.ModelOverrides.APIEndpointURL == ""
}

func modelHasUsableAPIEnvKey(info ModelInfo) bool {
	return info.APIEnvKey != "" && os.Getenv(info.APIEnvKey) != ""
}

func providerKeyFallbackSuppressed(info ModelInfo) bool {
	if !modelEligibleForProviderSubscription(info) || !ProviderSubscriptionRequired(info.ProviderID) {
		return false
	}
	return !ProviderHasSubscription(info.ProviderID)
}

func modelHasEligibleProviderSubscription(id ModelID) bool {
	info := GetModelInfo(id)
	if !modelEligibleForProviderSubscription(info) {
		return false
	}
	return ProviderHasSubscription(info.ProviderID)
}

func deriveModelID(pid ProviderID, providerModelID string) ModelID {
	if providerModelID == "" {
		return ModelIDUnknown
	}

	switch pid {
	case ProviderIDAnthropic:
		withoutDateSuffix := anthropicVersionSuffix.ReplaceAllString(providerModelID, "")
		withoutPrefix := strings.TrimPrefix(withoutDateSuffix, "claude-")
		normalized := dashBetweenDigits.ReplaceAllString(withoutPrefix, "${1}.${2}")
		return ModelID(normalized)
	default:
		return ModelID(providerModelID)
	}
}

// ensureUniqueModelIDLocked returns an unused user-visible model ID for a non-empty candidate.
//
// The caller must hold modelsMu for writing. The function returns ModelIDUnknown unchanged and does not register the returned ID.
func ensureUniqueModelIDLocked(pid ProviderID, candidate ModelID, providerModelID string) ModelID {
	if candidate == ModelIDUnknown {
		return candidate
	}
	if _, exists := modelsByID[candidate]; !exists {
		return candidate
	}

	if providerModelID != "" {
		fallback := ModelID(string(pid) + "/" + providerModelID)
		if _, exists := modelsByID[fallback]; !exists {
			return fallback
		}
	}

	base := string(candidate)
	if base == "" {
		base = "model"
	}

	for i := 1; ; i++ {
		alt := ModelID(fmt.Sprintf("%s#%d", base, i))
		if _, exists := modelsByID[alt]; !exists {
			return alt
		}
	}
}
