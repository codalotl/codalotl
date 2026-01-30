package llmcomplete

import (
	"fmt"
	"github.com/codalotl/codalotl/internal/llmcomplete/internal/modellist"
	"os"
	"strings"
)

// GOAL:
//   - support all providers with type=openai. From what I can see, this is the only omni type with multiple impls.
//   - support "custom" openai type somehow. Env variables I guess?
//   - Build models that also come bundled with params. I want gpt-5-minimal, gpt-5-low, gpt-5-medium,gpt-5-high. We can build this in for very popular models.
//   - for other openai-type models, provide a way in configuration to set certain params (ex: reasoning effort)
//   - I want to do less when new models drop. I want to gobble down the models with modellist, and have 90% of things "just work".
//   - For example: if openai drops a new gpt-5.1, I don't want to go write new lines. (but I can run the importer)
//   - Decision: hard code only certain models to splat the reasoningEffort onto.
//   - For openAI, it will be modellist's default large model.

// DECISION: each model in llmcomplete will have a unique public ID that can reference it. This ID isn't the ID in modellist.Model#ID (both openAI and Azure have "gpt-5" model).
// DECISION: I don't want compiled-code model IDs (current/old code has ModelOpenAI50 Model = "openai-gpt-5.0"). I want dynamic, string-creatable IDs.

// From a UX point of view, I can make codalotl.json have a key "custommodels": [{...}]
// Each custom model defined here looks like this: {id: "gpt-5-low-temp", type: "openai", modelid: "gpt-5", url: "openai.com/v1", reasoningEffort: "abc", temp: 3}
// Then this model is available with id gpt-5-low-temp
// This can work for anthropic or any custom model too. It's how you override params for anthropic, or any other provider.
// Finally set preferredmodel: your-custom-id, or pass -model=your-custom-id.

// Question:
// How do we actually curate a list of models we want to expose?
// - for instance, huggingface is a lot, and i don't know if huggingface is used commonly at all.
// DECISION:
// - For now, curate top models from openai, anthropic, xai, gemini.
// - do NOT add models (listable via `codalotl models`) from, eg, huggingface/openrouter.
// - typing `codalotl models` will DOCUMENT that you can add custom models via the mechanism above, and give an example using openrouter.
// - this will also list the providers that are openAI compatable where this is supported:

// Question:
// What do we do with GetModel(provider Provider, quality ProviderQuality) stuff?
// DECISION: kill it. It's not used.

// NOTE: I NEED A WAY TO GET DEFAULT MODEL FOR PROVIDER

// GO FORWARD PLAN:
// - Make this file be the API that I will try to drop into llmcomplete.

// ModelID is a user-visible ID for a model from the perspective of consumers of this package. It is NOT (necessarily) the same as the model id send to API endpoints.
// Consumers can create their own ModelID and register AddCustomModel, which bundles an provider model as well as a set of parameters.
// This also lets us/consumers alias long/awkward ids with nicer ones (ex: "claude-sonnet-4-5" vs "claude-sonnet-4-5-20250929").
type ModelID string

// NOTE: I don't want to have ModelIDXyz constants for all our models, because I want them to be more dynamic. I don't want to keep changing them
// every time a model is added or deprecated. These things move fast.

const ModelIDUnknown ModelID = ""

type model struct {
	// User visible ID (eg, they'd type -model some-id)
	id ModelID

	typ        ProviderType
	providerID ProviderID
	modelID    string

	providerObj *modellist.Provider // providerObj is the modellist provider
	modelObj    *modellist.Model    // modelObj is the modellist model

	isDefault bool // true if this is the default model for the provider

	ModelOverrides
}

type ModelOverrides struct {
	APIActualKey    string // ex: "123-456"
	APIKeyEnv       string // ex: "$ANTHROPIC_API_KEY" or "ANTHROPIC_API_KEY"
	APIEndpointURL  string // ex: "https://api.anthropic.com" or "https://api.openai.com/v1"
	ReasoningEffort string // ex: "medium"
}

type ProviderID = modellist.ProviderID
type ProviderType = modellist.Type

const (
	ProviderIDUnknown    ProviderID = ""
	ProviderIDOpenAI     ProviderID = "openai"
	ProviderIDAnthropic  ProviderID = "anthropic"
	ProviderIDGemini     ProviderID = "gemini"
	ProviderIDXAI        ProviderID = "xai"
	ProviderIDOpenRouter ProviderID = "openrouter"
	ProviderIDHugginFace ProviderID = "huggingface"
	ProviderIDDeepseek   ProviderID = "deepseek"
	ProviderIDGroq       ProviderID = "groq"
	ProviderIDZAI        ProviderID = "zai"
)

// AllProvidersIDs are all provider ids. They are sorted by my personal opinion of importance.
var AllProvidersIDs = []ProviderID{
	ProviderIDOpenAI,
	ProviderIDXAI,
	ProviderIDAnthropic,
	ProviderIDGemini,
	ProviderIDOpenRouter,
	ProviderIDHugginFace,
	ProviderIDDeepseek,
	ProviderIDGroq,
	ProviderIDZAI,
}

func ModelIDIsValid(id ModelID) bool {
	for _, m := range availableModels {
		if m.id == id {
			return true
		}
	}
	return false
}

func DefaultModelIDForProvider(providerID ProviderID) ModelID {
	for _, m := range availableModels {
		if m.providerID == providerID && m.isDefault {
			return m.id
		}
	}
	return ModelIDUnknown
}

func ProviderIDForModelID(id ModelID) ProviderID {
	for _, m := range availableModels {
		if m.id == id {
			return m.providerID
		}
	}
	return ProviderIDUnknown
}

var availableModels []model

// AddCustomModel adds the custom model to the available models. id is an opaque identifier that can be referred to later from consumers of this package.
// providerID must match a known provider (note: for truly custom models, just say it's openai, or whatever shape the API is, and use custom URL in overrides).
// providerModelID is the API parameter sent to the LLM provider for 'model' - matches API provider docs (ex: "claude-opus-4-20250514").
//
// It returns an error if:
//   - invalid id/providerID
//   - duplicate id
//
// However, as long as the ID is unique, it can duplicate model/parameters.
func AddCustomModel(id ModelID, providerID ProviderID, providerModelID string, overrides ModelOverrides) error {
	// Validate inputs
	if id == "" {
		return fmt.Errorf("model ID cannot be empty")
	}
	if providerID == "" {
		return fmt.Errorf("provider ID cannot be empty")
	}
	if providerModelID == "" {
		return fmt.Errorf("provider model ID cannot be empty")
	}

	// Check for duplicate ID
	for _, m := range availableModels {
		if m.id == id {
			return fmt.Errorf("model ID already exists: %s", id)
		}
	}

	// Find the provider
	providers := modellist.GetProviders()
	provider := findProvider(providers, providerID)
	if provider == nil {
		return fmt.Errorf("provider not found: %s", providerID)
	}
	typ := provider.Type

	// Try to find existing model
	var modelObj *modellist.Model
	for _, m := range provider.Models {
		if m.ID == providerModelID {
			modelObj = &m
			break
		}
	}

	// Create the model entry
	newModel := model{
		id:             id,
		typ:            typ,
		providerID:     providerID,
		modelID:        providerModelID,
		providerObj:    provider,
		modelObj:       modelObj,
		isDefault:      false, // Custom models are not defaults
		ModelOverrides: overrides,
	}

	// Add to available models
	availableModels = append(availableModels, newModel)

	return nil
}

func findProvider(providers []modellist.Provider, providerID ProviderID) *modellist.Provider {
	for _, p := range providers {
		if p.ID == providerID {
			pVar := p // paranoid about addressing loop var
			return &pVar
		}
	}
	return nil
}

// appendModelIfMissing appends m to models and returns the result, unless:
//   - m.id is already in models
//   - m.id is unique but typ/providerID/modelID/modelParams are all the same
func appendModelIfMissing(models []model, m model) []model {
	for _, mod := range models {
		if mod.id == m.id {
			return models
		}
		if mod.typ == m.typ && mod.providerID == m.providerID && mod.modelID == m.modelID && mod.ModelOverrides == m.ModelOverrides {
			return models
		}
	}

	return append(models, m)
}

func modelFromModellist(visibleID ModelID, provider *modellist.Provider, m *modellist.Model) model {
	if provider == nil || m == nil {
		return model{}
	}
	return model{
		id:          visibleID,
		providerObj: provider,
		modelObj:    m,
		typ:         provider.Type,
		providerID:  provider.ID,
		modelID:     m.ID,
	}
}

// setModelIsDefault set's IsDefault=true the model with matching id. It unsets IsDefault for all other models of the same provider ID.
func setModelIsDefault(models []model, id ModelID) []model {
	result := make([]model, len(models))
	var targetProvider ProviderID
	var found bool

	// First pass: find the target model and its provider
	for i := range models {
		result[i] = models[i]
		if result[i].id == id {
			result[i].isDefault = true
			targetProvider = result[i].providerID
			found = true
		}
	}

	// Second pass: unset isDefault for other models of the same provider
	if found {
		for i := range result {
			if result[i].providerID == targetProvider && result[i].id != id {
				result[i].isDefault = false
			}
		}
	}

	return result
}

func modelFromModellistWithReasoning(visibleID ModelID, provider *modellist.Provider, m *modellist.Model, reasoningEffort string) model {
	mod := modelFromModellist(visibleID, provider, m)
	mod.ReasoningEffort = reasoningEffort
	return mod
}

// init sets models. If there's anything unexpected that happens (ex: openai model is missing), it will print to stderr but not panic.
func init() {

	var availModelsSoFar []model
	providers := modellist.GetProviders()

	// Each is in closures so return just skips the provider

	// OpenAI:
	func() {
		provider := findProvider(providers, "openai")
		if provider == nil {
			fmt.Fprintf(os.Stderr, "COULD NOT FIND PROVIDER FOR OPENAI")
			return
		}

		topModel := provider.DefaultLargeModel()
		if topModel == (modellist.Model{}) {
			fmt.Fprintf(os.Stderr, "COULD NOT FIND DEFAULT LARGE MODEL FOR OPENAI")
			return
		}

		if !topModel.CanReason {
			fmt.Fprintf(os.Stderr, "TOP OPENAI MODEL CANT REASON")
			return
		}

		// I've debated with myself about naming this "gpt-5-medium" vs "gpt-5" vs doing both.
		// I've settled on "gpt-5" to optimize for the user typing `-model gpt-5`.
		// Even though "medium" is in the slice of efforts below, it won't get added due to appendModelIfMissing.
		availModelsSoFar = append(availModelsSoFar, modelFromModellistWithReasoning(ModelID(topModel.ID), provider, &topModel, "medium"))
		for _, reasoningEffort := range []string{"minimal", "medium", "low", "high"} {
			availModelsSoFar = appendModelIfMissing(availModelsSoFar, modelFromModellistWithReasoning(ModelID(topModel.ID+"-"+reasoningEffort), provider, &topModel, reasoningEffort))
		}

		for _, m := range provider.Models {
			if !m.IsLegacy {
				availModelsSoFar = appendModelIfMissing(availModelsSoFar, modelFromModellist(ModelID(m.ID), provider, &m))
			}
		}

		availModelsSoFar = setModelIsDefault(availModelsSoFar, ModelID(topModel.ID))
	}()

	// Anthropic:
	func() {
		sanitizeAnthropicID := func(visibleID string) ModelID {
			// Strip off the date suffix if present (e.g., "claude-sonnet-4-5-20250929" -> "claude-sonnet-4-5")
			parts := strings.Split(visibleID, "-")
			if len(parts) > 1 {
				lastPart := parts[len(parts)-1]
				// Check if the last part is exactly 8 digits (YYYYMMDD format)
				if len(lastPart) == 8 {
					allDigits := true
					for _, r := range lastPart {
						if r < '0' || r > '9' {
							allDigits = false
							break
						}
					}
					if allDigits {
						// Remove the date part
						return ModelID(strings.Join(parts[:len(parts)-1], "-"))
					}
				}
			}
			return ModelID(visibleID)
		}

		provider := findProvider(providers, "anthropic")
		if provider == nil {
			fmt.Fprintf(os.Stderr, "COULD NOT FIND PROVIDER FOR XAI")
			return
		}

		topModel := provider.DefaultLargeModel()
		if topModel == (modellist.Model{}) {
			fmt.Fprintf(os.Stderr, "COULD NOT FIND DEFAULT LARGE MODEL FOR ANTHROPIC")
			return
		}

		for _, m := range provider.Models {
			if !m.IsLegacy {
				availModelsSoFar = appendModelIfMissing(availModelsSoFar, modelFromModellist(sanitizeAnthropicID(m.ID), provider, &m))
			}
		}

		availModelsSoFar = setModelIsDefault(availModelsSoFar, sanitizeAnthropicID(topModel.ID))
	}()

	// XAI:
	func() {
		provider := findProvider(providers, "xai")
		if provider == nil {
			fmt.Fprintf(os.Stderr, "COULD NOT FIND PROVIDER FOR XAI")
			return
		}

		topModel := provider.DefaultLargeModel()
		if topModel == (modellist.Model{}) {
			fmt.Fprintf(os.Stderr, "COULD NOT FIND DEFAULT LARGE MODEL FOR XAI")
			return
		}

		for _, m := range provider.Models {
			if !m.IsLegacy {
				availModelsSoFar = appendModelIfMissing(availModelsSoFar, modelFromModellist(ModelID(m.ID), provider, &m))
			}
		}

		availModelsSoFar = setModelIsDefault(availModelsSoFar, ModelID(topModel.ID))
	}()

	// Gemini:
	func() {
		provider := findProvider(providers, "gemini")
		if provider == nil {
			fmt.Fprintf(os.Stderr, "COULD NOT FIND PROVIDER FOR GEMINI")
			return
		}

		topModel := provider.DefaultLargeModel()
		if topModel == (modellist.Model{}) {
			fmt.Fprintf(os.Stderr, "COULD NOT FIND DEFAULT LARGE MODEL FOR GEMINI")
			return
		}

		for _, m := range provider.Models {
			if !m.IsLegacy {
				availModelsSoFar = appendModelIfMissing(availModelsSoFar, modelFromModellist(ModelID(m.ID), provider, &m))
			}
		}

		availModelsSoFar = setModelIsDefault(availModelsSoFar, ModelID(topModel.ID))
	}()

	availableModels = availModelsSoFar
}

func getModelByID(id ModelID) (model, bool) {
	for _, m := range availableModels {
		if m.id == id {
			return m, true
		}
	}
	return model{}, false
}

func modelOrDefault(id ModelID) model {
	if m, ok := getModelByID(id); ok {
		return m
	}

	fallbackID := DefaultModelIDForProvider(ProviderIDOpenAI)
	if fallbackID != ModelIDUnknown {
		if m, ok := getModelByID(fallbackID); ok {
			return m
		}
	}

	if len(availableModels) > 0 {
		return availableModels[0]
	}

	return model{}
}

// AvailableModelIDs returns the list of user-visible model IDs registered with llmcomplete.
func AvailableModelIDs() []ModelID {
	ids := make([]ModelID, 0, len(availableModels))
	for _, m := range availableModels {
		ids = append(ids, m.id)
	}
	return ids
}

// ModelIDOrDefault returns id if it is valid. Otherwise it returns the default
// model for OpenAI providers (or the first available model, if none is set).
func ModelIDOrDefault(id ModelID) ModelID {
	if ModelIDIsValid(id) {
		return id
	}

	fallbackID := DefaultModelIDForProvider(ProviderIDOpenAI)
	if fallbackID != ModelIDUnknown {
		return fallbackID
	}

	if len(availableModels) > 0 {
		return availableModels[0].id
	}

	return ModelIDUnknown
}
