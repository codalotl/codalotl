package llmmodel

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func modelIDsByProvider(pid ProviderID) []ModelID {
	ids := AvailableModelIDs()
	out := make([]ModelID, 0, len(ids))
	for _, id := range ids {
		if GetModelInfo(id).ProviderID == pid {
			out = append(out, id)
		}
	}
	return out
}

func defaultModelIDsByProvider(pid ProviderID) []ModelID {
	ids := AvailableModelIDs()
	out := make([]ModelID, 0, 1)
	for _, id := range ids {
		if info := GetModelInfo(id); info.ProviderID == pid && info.IsDefault {
			out = append(out, id)
		}
	}
	return out
}

func TestInspectingDefaultModels(t *testing.T) {
	t.Skip("this is for debugging")
	for _, mid := range AvailableModelIDs() {
		m := GetModelInfo(mid)
		fmt.Printf("%s:\n", mid)
		fmt.Printf("  provider model ID: %s\n", m.ProviderModelID)
		fmt.Printf("  provider ID: %s\n", m.ProviderID)
		fmt.Printf("  supported API types: %v\n", m.SupportedTypes)
		if m.IsDefault {
			fmt.Printf("  is default: true\n")
		}
		fmt.Printf("  context window: %d tokens\n", m.ContextWindow)
		fmt.Printf("  max output: %d tokens\n", m.MaxOutput)
		if m.CostPer1MIn > 0 {
			fmt.Printf("  cost per 1M input tokens: $%.4f\n", m.CostPer1MIn)
		}
		if m.CostPer1MOut > 0 {
			fmt.Printf("  cost per 1M output tokens: $%.4f\n", m.CostPer1MOut)
		}
		if m.CostPer1MInCached > 0 {
			fmt.Printf("  cost per 1M cached input tokens: $%.4f\n", m.CostPer1MInCached)
		}
		if m.CostPer1MInSaveToCache > 0 {
			fmt.Printf("  cost per 1M saved cache tokens: $%.4f\n", m.CostPer1MInSaveToCache)
		}
		if m.CanReason {
			fmt.Printf("  can reason: true\n")
		}
		if m.SupportsImages {
			fmt.Printf("  supports images: true\n")
		}
		fmt.Println()
	}
}

func TestDefaultModelsLoaded(t *testing.T) {
	// OpenAI is expected to always be usable in this repo (it's our default codepath),
	// so keep stronger assertions for it.
	gpt5 := DefaultModel
	require.Equal(t, ModelID("gpt-5.4-high"), gpt5)
	require.True(t, gpt5.Valid())

	gptInfo := GetModelInfo(gpt5)
	require.Equal(t, ProviderIDOpenAI, gptInfo.ProviderID)
	require.Equal(t, "gpt-5.4", gptInfo.ProviderModelID)
	require.Equal(t, "high", gptInfo.ReasoningEffort)
	require.True(t, gptInfo.IsDefault)
	require.Equal(t, gpt5, ProviderIDOpenAI.DefaultModel())
	require.Equal(t, []ProviderAPIType{ProviderTypeOpenAIResponses}, gptInfo.SupportedTypes)

	// Other providers can legitimately have models marked as legacy (and therefore not registered)
	// until we have proper support. These checks validate internal consistency without requiring
	// any particular model IDs to be present.
	for _, pid := range []ProviderID{ProviderIDAnthropic, ProviderIDGemini, ProviderIDXAI} {
		ids := modelIDsByProvider(pid)
		def := pid.DefaultModel()

		if len(ids) == 0 {
			require.Equal(t, ModelIDUnknown, def, "expected %q to have no default when it has no registered models", pid)
			continue
		}

		require.True(t, def.Valid(), "expected %q default model to be valid", pid)
		require.Equal(t, pid, def.ProviderID())

		explicitDefaults := defaultModelIDsByProvider(pid)
		if len(explicitDefaults) > 0 {
			require.Contains(t, explicitDefaults, def, "expected %q default model to be one of the explicit defaults", pid)
		} else {
			// If a provider has no explicit default registered (ex: the configured default model is legacy),
			// we fall back to the first non-legacy model we registered for that provider.
			require.Equal(t, ids[0], def)
		}
	}

	require.False(t, ModelID("claude-sonnet-4-6").Valid())
	require.False(t, ModelID("sonnet-4-6").Valid())
	anthropicOpus := ModelID("opus-4.6")
	require.True(t, anthropicOpus.Valid())
	require.Equal(t, int64(1000000), GetModelInfo(anthropicOpus).ContextWindow)
	anthropicSonnet := ModelID("sonnet-4.6")
	require.True(t, anthropicSonnet.Valid())
	anthropicSonnetInfo := GetModelInfo(anthropicSonnet)
	require.Equal(t, ProviderIDAnthropic, anthropicSonnetInfo.ProviderID)
	require.Equal(t, "claude-sonnet-4-6", anthropicSonnetInfo.ProviderModelID)
	require.Equal(t, ProviderIDAnthropic, anthropicSonnet.ProviderID())
	require.Equal(t, []ProviderAPIType{ProviderTypeAnthropic}, anthropicSonnetInfo.SupportedTypes)
	require.Equal(t, int64(1000000), anthropicSonnetInfo.ContextWindow)
	anthropicHaiku := ModelID("haiku-4.5")
	require.True(t, anthropicHaiku.Valid())
	require.Equal(t, int64(200000), GetModelInfo(anthropicHaiku).ContextWindow)

	gemini := ModelID("gemini-2.5-pro")
	if gemini.Valid() {
		geminiInfo := GetModelInfo(gemini)
		require.Equal(t, ProviderIDGemini, geminiInfo.ProviderID)
		require.Equal(t, []ProviderAPIType{ProviderTypeGemini}, geminiInfo.SupportedTypes)
	}

	grok := ModelID("grok-4")
	if grok.Valid() {
		grokInfo := GetModelInfo(grok)
		require.Equal(t, ProviderIDXAI, grokInfo.ProviderID)
	}

	require.False(t, ModelID("gpt-5-codex").Valid())
	require.False(t, ModelID("gpt-5.4").Valid())
	require.False(t, ModelID("gpt-5.3-codex").Valid())
	require.False(t, ModelID("gpt-5.1-codex").Valid())
	require.False(t, ModelID("gpt-5.3-codex-minimal").Valid())
	require.False(t, ModelID("gpt-5.4-minimal").Valid())

	codexXhigh := ModelID("gpt-5.3-codex-xhigh")
	codexHigh := ModelID("gpt-5.3-codex-high")
	require.True(t, codexXhigh.Valid())
	codexXhighInfo := GetModelInfo(codexXhigh)
	require.Equal(t, ProviderIDOpenAI, codexXhighInfo.ProviderID)
	require.Equal(t, "gpt-5.3-codex", codexXhighInfo.ProviderModelID)
	require.Equal(t, []ProviderAPIType{ProviderTypeOpenAIResponses}, codexXhighInfo.SupportedTypes)
	require.Equal(t, "xhigh", codexXhighInfo.ReasoningEffort)

	require.True(t, codexHigh.Valid())
	require.Equal(t, "high", GetModelInfo(codexHigh).ReasoningEffort)

	gptXhigh := ModelID("gpt-5.4-xhigh")
	require.True(t, gptXhigh.Valid())
	gptXhighInfo := GetModelInfo(gptXhigh)
	require.Equal(t, ProviderIDOpenAI, gptXhighInfo.ProviderID)
	require.Equal(t, "gpt-5.4", gptXhighInfo.ProviderModelID)
	require.Equal(t, []ProviderAPIType{ProviderTypeOpenAIResponses}, gptXhighInfo.SupportedTypes)
	require.Equal(t, "xhigh", gptXhighInfo.ReasoningEffort)

	require.Equal(t, gpt5, ModelIDOrFallback(ModelIDUnknown))
	require.Equal(t, gpt5, ModelIDOrFallback(ModelID("unknown-model")))

	envVars := ProviderKeyEnvVars()
	require.Equal(t, "OPENAI_API_KEY", envVars[ProviderIDOpenAI])

	t.Setenv("ANTHROPIC_API_KEY", "")
	require.False(t, EnvHasDefaultKey(ProviderIDAnthropic))
	t.Setenv("ANTHROPIC_API_KEY", "abc123")
	require.True(t, EnvHasDefaultKey(ProviderIDAnthropic))
}

func TestAddCustomModelCopiesProviderData(t *testing.T) {
	customID := ModelID("custom-anthropic-claude-opus")
	require.False(t, customID.Valid())

	err := AddCustomModel(customID, ProviderIDAnthropic, "claude-opus-4-6", ModelOverrides{
		ReasoningEffort: "low",
		ServiceTier:     "priority",
	})
	require.NoError(t, err)
	require.True(t, customID.Valid())

	info := GetModelInfo(customID)
	require.Equal(t, ProviderIDAnthropic, info.ProviderID)
	require.Equal(t, "claude-opus-4-6", info.ProviderModelID)
	require.InDelta(t, 5.0, info.CostPer1MIn, 0)
	require.InDelta(t, 25.0, info.CostPer1MOut, 0)
	require.False(t, info.IsDefault)
	require.Equal(t, []ProviderAPIType{ProviderTypeAnthropic}, info.SupportedTypes)
	require.Equal(t, "low", info.ReasoningEffort)
	require.Equal(t, "priority", info.ServiceTier)
	require.Contains(t, AvailableModelIDs(), customID)
}

func TestGetAPIKeyPrecedence(t *testing.T) {
	id := DefaultModel
	require.True(t, id.Valid())

	ConfigureProviderKey(ProviderIDOpenAI, "")
	t.Cleanup(func() {
		ConfigureProviderKey(ProviderIDOpenAI, "")
	})

	t.Setenv("OPENAI_API_KEY", "")
	require.Equal(t, "", GetAPIKey(id))

	t.Setenv("OPENAI_API_KEY", "default")
	require.Equal(t, "default", GetAPIKey(id))

	ConfigureProviderKey(ProviderIDOpenAI, "configured")
	require.Equal(t, "configured", GetAPIKey(id))

	ConfigureProviderKey(ProviderIDOpenAI, "")
	require.Equal(t, "default", GetAPIKey(id))

	customEnvID := ModelID("custom-openai-env")
	t.Setenv("ALT_OPENAI_KEY", "")
	err := AddCustomModel(customEnvID, ProviderIDOpenAI, "gpt-5.4", ModelOverrides{APIEnvKey: "$ALT_OPENAI_KEY"})
	require.NoError(t, err)
	t.Setenv("ALT_OPENAI_KEY", "alt")
	require.Equal(t, "alt", GetAPIKey(customEnvID))

	customActualID := ModelID("custom-openai-actual")
	err = AddCustomModel(customActualID, ProviderIDOpenAI, "gpt-5.4", ModelOverrides{APIActualKey: "literal"})
	require.NoError(t, err)
	ConfigureProviderKey(ProviderIDOpenAI, "configured2")
	t.Setenv("ALT_OPENAI_KEY", "alt2")
	require.Equal(t, "literal", GetAPIKey(customActualID))
}

func TestAvailableModelIDsWithAPIKeyAndProviderHasConfiguredKey(t *testing.T) {
	// Clear any in-memory overrides that could leak in from other tests.
	ConfigureProviderKey(ProviderIDOpenAI, "")
	ConfigureProviderKey(ProviderIDAnthropic, "")
	ConfigureProviderKey(ProviderIDGemini, "")
	ConfigureProviderKey(ProviderIDXAI, "")
	t.Cleanup(func() {
		ConfigureProviderKey(ProviderIDOpenAI, "")
		ConfigureProviderKey(ProviderIDAnthropic, "")
		ConfigureProviderKey(ProviderIDGemini, "")
		ConfigureProviderKey(ProviderIDXAI, "")
	})

	// No env keys => nothing should be considered configured.
	env := ProviderKeyEnvVars()
	for _, pid := range AllProviderIDs {
		if k := env[pid]; k != "" {
			t.Setenv(k, "")
		}
	}

	require.False(t, ProviderHasConfiguredKey(ProviderIDOpenAI))
	require.False(t, ProviderHasConfiguredKey(ProviderIDAnthropic))
	require.False(t, ProviderHasConfiguredKey(ProviderIDGemini))
	require.False(t, ProviderHasConfiguredKey(ProviderIDXAI))

	// With no provider keys, the only models that should appear are ones with per-model
	// overrides (ex: APIActualKey / APIEnvKey).
	for _, id := range AvailableModelIDsWithAPIKey() {
		info := GetModelInfo(id)
		require.NotEqual(t, ModelIDUnknown, info.ID)
		require.False(t, ProviderHasConfiguredKey(info.ProviderID))
		require.True(t,
			info.APIActualKey != "" || (info.APIEnvKey != "" && os.Getenv(info.APIEnvKey) != ""),
			"unexpected key source for model %q (provider %q)", id, info.ProviderID,
		)
	}

	// Configure only OpenAI via env => model list should only contain OpenAI models.
	require.NotEmpty(t, env[ProviderIDOpenAI])
	t.Setenv(env[ProviderIDOpenAI], "openai-key")

	require.True(t, ProviderHasConfiguredKey(ProviderIDOpenAI))
	require.False(t, ProviderHasConfiguredKey(ProviderIDAnthropic))

	for _, id := range AvailableModelIDsWithAPIKey() {
		info := GetModelInfo(id)
		require.NotEqual(t, ModelIDUnknown, info.ID)
		switch info.ProviderID {
		case ProviderIDOpenAI:
			// ok
		case ProviderIDAnthropic, ProviderIDGemini, ProviderIDXAI:
			require.True(t,
				info.APIActualKey != "" || (info.APIEnvKey != "" && os.Getenv(info.APIEnvKey) != ""),
				"model %q unexpectedly available without %q being configured", id, info.ProviderID,
			)
		default:
			t.Fatalf("unexpected provider %q for model %q", info.ProviderID, id)
		}
	}

	// Configure Anthropic via ConfigureProviderKey (not env) => list should now include Anthropic models too.
	ConfigureProviderKey(ProviderIDAnthropic, "anthropic-key")
	require.True(t, ProviderHasConfiguredKey(ProviderIDAnthropic))

	seenOpenAI := false
	seenAnthropic := false
	for _, id := range AvailableModelIDsWithAPIKey() {
		switch id.ProviderID() {
		case ProviderIDOpenAI:
			seenOpenAI = true
		case ProviderIDAnthropic:
			seenAnthropic = true
		default:
			t.Fatalf("unexpected provider %q in AvailableModelIDsWithAPIKey", id.ProviderID())
		}
	}
	require.True(t, seenOpenAI)
	require.True(t, seenAnthropic)
}
