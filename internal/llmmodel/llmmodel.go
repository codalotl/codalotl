package llmmodel

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sync"
)

// ModelID is a user-visible ID for a model from the perspective of consumers of this package. It is NOT (necessarily) the same as the model ID sent to API endpoints.
// Consumers can create/register their own ModelID with AddCustomModel, which bundles a provider model as well as a set of parameters. This also lets this package
// and consumers alias long/awkward ids with nicer ones (ex: "claude-sonnet-4-5" vs "claude-sonnet-4-5-20250929").
type ModelID string

// DefaultModel is a good default model. It can be used in tests or in production code.
//
// Applications probably want to define their own default model.
const DefaultModel ModelID = "gpt-5.2"

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

type ModelOverrides struct {
	APIActualKey    string // ex: "123-456"
	APIEnvKey       string // ex: "$ANTHROPIC_API_KEY" or "ANTHROPIC_API_KEY"
	APIEndpointURL  string // ex: "https://api.anthropic.com" or "https://api.openai.com/v1"
	ReasoningEffort string // ex: "medium"
	ServiceTier     string // ex: "priority"
}

type ProviderID string

// DefaultModel returns the default model ID for pid.
func (pid ProviderID) DefaultModel() ModelID {
	modelsMu.RLock()
	defer modelsMu.RUnlock()
	return providerDefaults[pid]
}

// ProviderAPIType identifies one API "shape" a provider supports. Providers can expose multiple API types simultaneously (ex: OpenAI exposes both Responses and
// Completions).
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

// AllProviderIDs are all provider ids. They are sorted by my personal opinion of importance.
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
// This method can be used in cases where the consumer must talk to *some* valid model, but their current model id might be unset or invalid.
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

type ModelInfo struct {
	ID              ModelID
	ProviderID      ProviderID
	SupportedTypes  []ProviderAPIType
	ProviderModelID string // the model identifier used in API requests.
	IsDefault       bool
	APIEndpointURL  string

	// Note on pricing: uniformly modeling pricing across all providers is fraught. These numbers serve as rough guidelines. Some providers might be modeled very poorly.
	// As of 2025/10/23:
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
	SupportsImages         bool    // SupportsImages reports whether the model accepts image inputs.
	ModelOverrides
}

// GetModelInfo returns information for the corresponding model ID.
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

// GetAPIKey returns the API key for the model with id ("" if not found). This is the precedence:
//  1. ModelInfo.ModelOverrides.APIActualKey
//  2. Env[ModelInfo.ModelOverrides.APIEnvKey]
//  3. Value from ConfigureProviderKey for id.ProviderID()
//  4. Env[ProviderKeyEnvVars()[id.ProviderID()]]
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

type providerConfigFile struct {
	ID             string                 `json:"id"`
	Types          []string               `json:"types"`
	APIEndpointURL string                 `json:"api_endpoint_url"`
	APIKey         string                 `json:"api_key"`
	DefaultModelID string                 `json:"default_model_id"`
	Models         []providerModelPayload `json:"models"`
}

type providerModelPayload struct {
	ID                     string  `json:"id"`
	CostPer1MIn            float64 `json:"cost_per_1m_in"`
	CostPer1MOut           float64 `json:"cost_per_1m_out"`
	CostPer1MInCached      float64 `json:"cost_per_1m_in_cached"`
	CostPer1MOutCached     float64 `json:"cost_per_1m_out_cached"`
	CostPer1MInSaveToCache float64 `json:"cost_per_1m_in_save_to_cache"`
	ContextWindow          int64   `json:"context_window"`
	MaxOutput              int64   `json:"max_output"`
	CanReason              bool    `json:"can_reason"`
	HasReasoningEffort     bool    `json:"has_reasoning_effort"`
	SupportsImages         bool    `json:"supports_images"`
	IsLegacy               bool    `json:"is_legacy"`
}

type providerData struct {
	ID                   ProviderID
	SupportedTypes       []ProviderAPIType
	APIEndpointURL       string
	DefaultProviderModel string
	APIKeyEnv            string
	Models               []providerModelPayload
	ModelByID            map[string]providerModelPayload
}

var (
	modelsMu             sync.RWMutex
	modelsByID           = make(map[ModelID]ModelInfo)
	modelOrder           []ModelID
	providerDefaults     = make(map[ProviderID]ModelID)
	providerEnvVars      = make(map[ProviderID]string)
	providerKeyOverrides = make(map[ProviderID]string)
	providerCatalog      = make(map[ProviderID]providerData)
)

var anthropicVersionSuffix = regexp.MustCompile(`-\d{6,}$`)

func init() {
	if err := loadProviders(); err != nil {
		panic(err)
	}
	registerPrimaryModels()
}

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

func registerPrimaryModels() {
	modelsMu.Lock()
	defer modelsMu.Unlock()

	type reasoningVariant struct {
		suffix string
		effort string
	}
	reasoningVariants := []reasoningVariant{
		{suffix: "minimal", effort: "minimal"},
		{suffix: "low", effort: "low"},
		{suffix: "medium", effort: "medium"},
		{suffix: "high", effort: "high"},
	}

	registerOpenAIReasoningVariants := func(provider providerData, m providerModelPayload, firstModel *ModelID, variantsCanBeDefault bool) {
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
				IsDefault:              variantsCanBeDefault && m.ID == provider.DefaultProviderModel && variant.suffix == "high",
				APIEndpointURL:         provider.APIEndpointURL,
				CostPer1MIn:            m.CostPer1MIn,
				CostPer1MOut:           m.CostPer1MOut,
				CostPer1MInCached:      m.CostPer1MInCached,
				CostPer1MInSaveToCache: m.CostPer1MInSaveToCache,
				ContextWindow:          m.ContextWindow,
				MaxOutput:              m.MaxOutput,
				CanReason:              m.CanReason,
				HasReasoningEffort:     m.HasReasoningEffort,
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

			if pid == ProviderIDOpenAI && m.ID == "gpt-5.1-codex" {
				registerOpenAIReasoningVariants(provider, m, &firstModel, true)
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
				SupportsImages:         m.SupportsImages,
			}

			if pid == ProviderIDOpenAI && m.ID == "gpt-5.2" {
				info.ModelOverrides.ReasoningEffort = "high"
			}

			modelsByID[unique] = info
			modelOrder = append(modelOrder, unique)

			if info.IsDefault && providerDefaults[pid] == ModelIDUnknown {
				providerDefaults[pid] = unique
			}

			if pid == ProviderIDOpenAI && m.ID == "gpt-5.2" {
				registerOpenAIReasoningVariants(provider, m, &firstModel, false)
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

func deriveModelID(pid ProviderID, providerModelID string) ModelID {
	if providerModelID == "" {
		return ModelIDUnknown
	}

	switch pid {
	case ProviderIDAnthropic:
		return ModelID(anthropicVersionSuffix.ReplaceAllString(providerModelID, ""))
	default:
		return ModelID(providerModelID)
	}
}

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
