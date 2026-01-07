package llmmodel

import (
	"fmt"
	"os"
	"strings"
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
	gpt5 := ModelID("gpt-5.2")
	require.True(t, gpt5.Valid())

	gptInfo := GetModelInfo(gpt5)
	require.Equal(t, ProviderIDOpenAI, gptInfo.ProviderID)
	require.Equal(t, "high", gptInfo.ReasoningEffort)
	require.True(t, gptInfo.IsDefault)
	require.Equal(t, gpt5, ProviderIDOpenAI.DefaultModel())
	require.Equal(t, []ProviderAPIType{ProviderTypeOpenAIResponses, ProviderTypeOpenAICompletions}, gptInfo.SupportedTypes)

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

	// If these well-known model IDs exist (i.e. not marked legacy), validate their mapping,
	// but don't require them to be present.
	claude := ModelID("claude-sonnet-4-5")
	if claude.Valid() {
		claudeInfo := GetModelInfo(claude)
		require.Equal(t, ProviderIDAnthropic, claudeInfo.ProviderID)
		require.True(t, strings.HasPrefix(claudeInfo.ProviderModelID, "claude-sonnet-4-5"))
		require.Equal(t, ProviderIDAnthropic, claude.ProviderID())
		require.Equal(t, []ProviderAPIType{ProviderTypeAnthropic}, claudeInfo.SupportedTypes)
	}

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
	require.False(t, ModelID("gpt-5.1-codex").Valid())
	codexMinimal := ModelID("gpt-5.1-codex-minimal")
	codexHigh := ModelID("gpt-5.1-codex-high")
	if codexMinimal.Valid() || codexHigh.Valid() {
		require.True(t, codexMinimal.Valid())
		codexMinimalInfo := GetModelInfo(codexMinimal)
		require.Equal(t, ProviderIDOpenAI, codexMinimalInfo.ProviderID)
		require.Equal(t, "gpt-5.1-codex", codexMinimalInfo.ProviderModelID)
		require.Equal(t, []ProviderAPIType{ProviderTypeOpenAIResponses}, codexMinimalInfo.SupportedTypes)
		require.Equal(t, "minimal", codexMinimalInfo.ReasoningEffort)

		require.True(t, codexHigh.Valid())
		require.Equal(t, "high", GetModelInfo(codexHigh).ReasoningEffort)
	}

	gptMinimal := ModelID("gpt-5.2-minimal")
	require.True(t, gptMinimal.Valid())
	gptMinimalInfo := GetModelInfo(gptMinimal)
	require.Equal(t, ProviderIDOpenAI, gptMinimalInfo.ProviderID)
	require.Equal(t, "gpt-5.2", gptMinimalInfo.ProviderModelID)
	require.Equal(t, []ProviderAPIType{ProviderTypeOpenAIResponses}, gptMinimalInfo.SupportedTypes)
	require.Equal(t, "minimal", gptMinimalInfo.ReasoningEffort)

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

	err := AddCustomModel(customID, ProviderIDAnthropic, "claude-opus-4-1-20250805", ModelOverrides{})
	require.NoError(t, err)
	require.True(t, customID.Valid())

	info := GetModelInfo(customID)
	require.Equal(t, ProviderIDAnthropic, info.ProviderID)
	require.Equal(t, "claude-opus-4-1-20250805", info.ProviderModelID)
	require.InDelta(t, 15.0, info.CostPer1MIn, 0)
	require.InDelta(t, 75.0, info.CostPer1MOut, 0)
	require.False(t, info.IsDefault)
	require.Equal(t, []ProviderAPIType{ProviderTypeAnthropic}, info.SupportedTypes)
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
	err := AddCustomModel(customEnvID, ProviderIDOpenAI, "gpt-5.2", ModelOverrides{APIEnvKey: "$ALT_OPENAI_KEY"})
	require.NoError(t, err)
	t.Setenv("ALT_OPENAI_KEY", "alt")
	require.Equal(t, "alt", GetAPIKey(customEnvID))

	customActualID := ModelID("custom-openai-actual")
	err = AddCustomModel(customActualID, ProviderIDOpenAI, "gpt-5.2", ModelOverrides{APIActualKey: "literal"})
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
