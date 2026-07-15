package llmmodel

import (
	"fmt"
	"os"
	"testing"
	"time"

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

func clearProviderAuthForTest(t *testing.T) {
	t.Helper()

	for _, pid := range AllProviderIDs {
		ConfigureProviderKey(pid, "")
		ClearProviderSubscription(pid)
		SetProviderSubscriptionRequired(pid, false)
	}
	for _, envKey := range ProviderKeyEnvVars() {
		if envKey != "" {
			t.Setenv(envKey, "")
		}
	}

	t.Cleanup(func() {
		for _, pid := range AllProviderIDs {
			ConfigureProviderKey(pid, "")
			ClearProviderSubscription(pid)
			SetProviderSubscriptionRequired(pid, false)
		}
	})
}

func validProviderSubscription(providerID ProviderID) ProviderSubscription {
	return ProviderSubscription{
		ProviderID:       providerID,
		AccessToken:      "access-token",
		AccountID:        "account-id",
		APIEndpointURL:   "https://chatgpt.com/backend-api/codex",
		ExpiresAt:        time.Now().Add(time.Hour),
		RequiresNoStore:  true,
		RootInstructions: true,
	}
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
	require.Equal(t, ModelID("gpt-5.6-sol-high"), gpt5)
	require.True(t, gpt5.Valid())

	gptInfo := GetModelInfo(gpt5)
	require.Equal(t, ProviderIDOpenAI, gptInfo.ProviderID)
	require.Equal(t, "gpt-5.6-sol", gptInfo.ProviderModelID)
	require.Equal(t, "high", gptInfo.ReasoningEffort)
	require.True(t, gptInfo.IsDefault)
	require.True(t, gptInfo.SupportsAutocompaction)
	require.Equal(t, gpt5, ProviderIDOpenAI.DefaultModel())
	require.Equal(t, []ProviderAPIType{ProviderTypeOpenAIResponses}, gptInfo.SupportedTypes)
	require.InDelta(t, 5.0, gptInfo.CostPer1MIn, 0)
	require.InDelta(t, 30.0, gptInfo.CostPer1MOut, 0)
	require.InDelta(t, 0.5, gptInfo.CostPer1MInCached, 0)
	require.Equal(t, int64(1050000), gptInfo.ContextWindow)
	require.Equal(t, int64(128000), gptInfo.MaxOutput)

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
	anthropicOpusInfo := GetModelInfo(anthropicOpus)
	require.Equal(t, int64(1000000), anthropicOpusInfo.ContextWindow)
	require.Equal(t, int64(128000), anthropicOpusInfo.MaxOutput)
	anthropicSonnet := ModelID("sonnet-4.6")
	require.True(t, anthropicSonnet.Valid())
	anthropicSonnetInfo := GetModelInfo(anthropicSonnet)
	require.Equal(t, ProviderIDAnthropic, anthropicSonnetInfo.ProviderID)
	require.Equal(t, "claude-sonnet-4-6", anthropicSonnetInfo.ProviderModelID)
	require.Equal(t, ProviderIDAnthropic, anthropicSonnet.ProviderID())
	require.Equal(t, []ProviderAPIType{ProviderTypeAnthropic}, anthropicSonnetInfo.SupportedTypes)
	require.Equal(t, int64(1000000), anthropicSonnetInfo.ContextWindow)
	require.Equal(t, int64(64000), anthropicSonnetInfo.MaxOutput)
	anthropicHaiku := ModelID("haiku-4.5")
	require.True(t, anthropicHaiku.Valid())
	anthropicHaikuInfo := GetModelInfo(anthropicHaiku)
	require.Equal(t, int64(200000), anthropicHaikuInfo.ContextWindow)
	require.Equal(t, int64(64000), anthropicHaikuInfo.MaxOutput)

	geminiModels := modelIDsByProvider(ProviderIDGemini)
	require.Len(t, geminiModels, 1)
	gemini := geminiModels[0]
	require.True(t, gemini.Valid())
	geminiInfo := GetModelInfo(gemini)
	require.Equal(t, ProviderIDGemini, geminiInfo.ProviderID)
	require.Equal(t, gemini, ProviderIDGemini.DefaultModel())
	require.Equal(t, []ProviderAPIType{ProviderTypeGemini}, geminiInfo.SupportedTypes)
	require.NotEmpty(t, geminiInfo.ProviderModelID)
	require.Equal(t, int64(1048576), geminiInfo.ContextWindow)
	require.Equal(t, int64(65536), geminiInfo.MaxOutput)
	require.True(t, geminiInfo.CanReason)
	require.True(t, geminiInfo.SupportsImages)
	require.InDelta(t, 2.0, geminiInfo.CostPer1MIn, 0)
	require.InDelta(t, 12.0, geminiInfo.CostPer1MOut, 0)
	require.InDelta(t, 0.2, geminiInfo.CostPer1MInCached, 0)

	grok := ModelID("grok-4")
	if grok.Valid() {
		grokInfo := GetModelInfo(grok)
		require.Equal(t, ProviderIDXAI, grokInfo.ProviderID)
	}

	removedOpenAIModels := []ModelID{
		"gpt-5.3-codex-high",
		"gpt-5.4-high",
		"gpt-5.5-high",
		"gpt-5-mini-high",
		"gpt-5-nano-high",
	}
	for _, id := range removedOpenAIModels {
		t.Run(string(id), func(t *testing.T) {
			require.False(t, id.Valid())
		})
	}

	gptXhigh := ModelID("gpt-5.6-sol-xhigh")
	require.True(t, gptXhigh.Valid())
	gptXhighInfo := GetModelInfo(gptXhigh)
	require.Equal(t, ProviderIDOpenAI, gptXhighInfo.ProviderID)
	require.Equal(t, "gpt-5.6-sol", gptXhighInfo.ProviderModelID)
	require.Equal(t, []ProviderAPIType{ProviderTypeOpenAIResponses}, gptXhighInfo.SupportedTypes)
	require.Equal(t, "xhigh", gptXhighInfo.ReasoningEffort)
	require.True(t, gptXhighInfo.SupportsAutocompaction)

	require.Equal(t, gpt5, ModelIDOrFallback(ModelIDUnknown))
	require.Equal(t, gpt5, ModelIDOrFallback(ModelID("unknown-model")))

	envVars := ProviderKeyEnvVars()
	require.Equal(t, "OPENAI_API_KEY", envVars[ProviderIDOpenAI])

	t.Setenv("ANTHROPIC_API_KEY", "")
	require.False(t, EnvHasDefaultKey(ProviderIDAnthropic))
	t.Setenv("ANTHROPIC_API_KEY", "abc123")
	require.True(t, EnvHasDefaultKey(ProviderIDAnthropic))
}

func TestGPT56OpenAIModelsLoaded(t *testing.T) {
	tests := []struct {
		providerModelID        string
		reasoningLevels        []string
		costPer1MIn            float64
		costPer1MOut           float64
		costPer1MInCached      float64
		costPer1MInSaveToCache float64
	}{
		{
			providerModelID:        "gpt-5.6-sol",
			reasoningLevels:        []string{"medium", "high", "xhigh"},
			costPer1MIn:            5,
			costPer1MOut:           30,
			costPer1MInCached:      0.5,
			costPer1MInSaveToCache: 6.25,
		},
		{
			providerModelID:        "gpt-5.6-terra",
			reasoningLevels:        []string{"high"},
			costPer1MIn:            2.5,
			costPer1MOut:           15,
			costPer1MInCached:      0.25,
			costPer1MInSaveToCache: 3.125,
		},
		{
			providerModelID:        "gpt-5.6-luna",
			reasoningLevels:        []string{"high"},
			costPer1MIn:            1,
			costPer1MOut:           6,
			costPer1MInCached:      0.1,
			costPer1MInSaveToCache: 1.25,
		},
	}

	for _, tt := range tests {
		t.Run(tt.providerModelID, func(t *testing.T) {
			require.False(t, ModelID(tt.providerModelID).Valid())

			for _, level := range tt.reasoningLevels {
				id := ModelID(fmt.Sprintf("%s-%s", tt.providerModelID, level))
				require.True(t, id.Valid())

				info := GetModelInfo(id)
				require.Equal(t, id, info.ID)
				require.Equal(t, ProviderIDOpenAI, info.ProviderID)
				require.Equal(t, tt.providerModelID, info.ProviderModelID)
				require.Equal(t, []ProviderAPIType{ProviderTypeOpenAIResponses}, info.SupportedTypes)
				require.Equal(t, id == DefaultModel, info.IsDefault)
				require.Equal(t, level, info.ReasoningEffort)
				require.InDelta(t, tt.costPer1MIn, info.CostPer1MIn, 0)
				require.InDelta(t, tt.costPer1MOut, info.CostPer1MOut, 0)
				require.InDelta(t, tt.costPer1MInCached, info.CostPer1MInCached, 0)
				require.InDelta(t, tt.costPer1MInSaveToCache, info.CostPer1MInSaveToCache, 0)
				require.Equal(t, int64(1050000), info.ContextWindow)
				require.Equal(t, int64(128000), info.MaxOutput)
				require.True(t, info.CanReason)
				require.True(t, info.HasReasoningEffort)
				require.True(t, info.SupportsAutocompaction)
				require.True(t, info.SupportsImages)
			}

			for _, level := range []string{"medium", "high", "xhigh"} {
				id := ModelID(fmt.Sprintf("%s-%s", tt.providerModelID, level))
				require.Equal(t, level == "high" || tt.providerModelID == "gpt-5.6-sol", id.Valid())
			}
		})
	}

	require.Equal(t, DefaultModel, ProviderIDOpenAI.DefaultModel())
}

func TestAddCustomModelCopiesOpenAIMetadata(t *testing.T) {
	providerModels := []string{"gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna"}
	for _, providerModelID := range providerModels {
		t.Run(providerModelID, func(t *testing.T) {
			customID := ModelID("custom-" + providerModelID)
			err := AddCustomModel(customID, ProviderIDOpenAI, providerModelID, ModelOverrides{ReasoningEffort: "high"})
			require.NoError(t, err)

			info := GetModelInfo(customID)
			require.Equal(t, providerModelID, info.ProviderModelID)
			require.Equal(t, "high", info.ReasoningEffort)
			require.True(t, info.CanReason)
			require.NotZero(t, info.ContextWindow)
			require.NotZero(t, info.MaxOutput)
		})
	}
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
	require.False(t, info.SupportsAutocompaction)
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
	err := AddCustomModel(customEnvID, ProviderIDOpenAI, "gpt-5.6-sol", ModelOverrides{APIEnvKey: "$ALT_OPENAI_KEY"})
	require.NoError(t, err)
	t.Setenv("ALT_OPENAI_KEY", "alt")
	require.Equal(t, "alt", GetAPIKey(customEnvID))

	customActualID := ModelID("custom-openai-actual")
	err = AddCustomModel(customActualID, ProviderIDOpenAI, "gpt-5.6-sol", ModelOverrides{APIActualKey: "literal"})
	require.NoError(t, err)
	ConfigureProviderKey(ProviderIDOpenAI, "configured2")
	t.Setenv("ALT_OPENAI_KEY", "alt2")
	require.Equal(t, "literal", GetAPIKey(customActualID))
	require.True(t, GetModelInfo(customActualID).SupportsAutocompaction)
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

func TestProviderSubscriptionRequiresUsableAuth(t *testing.T) {
	clearProviderAuthForTest(t)

	require.False(t, ProviderSubscriptionRequired(ProviderIDOpenAI))
	SetProviderSubscriptionRequired(ProviderIDOpenAI, true)
	require.True(t, ProviderSubscriptionRequired(ProviderIDOpenAI))
	SetProviderSubscriptionRequired(ProviderIDOpenAI, false)
	require.False(t, ProviderSubscriptionRequired(ProviderIDOpenAI))

	valid := validProviderSubscription(ProviderIDOpenAI)
	SetProviderSubscription(ProviderIDOpenAI, valid)

	sub, ok := GetProviderSubscription(ProviderIDOpenAI)
	require.True(t, ok)
	require.Equal(t, valid, sub)
	require.True(t, ProviderHasSubscription(ProviderIDOpenAI))

	ClearProviderSubscription(ProviderIDOpenAI)
	_, ok = GetProviderSubscription(ProviderIDOpenAI)
	require.False(t, ok)
	require.False(t, ProviderHasSubscription(ProviderIDOpenAI))

	tests := map[string]func(ProviderSubscription) ProviderSubscription{
		"provider mismatch": func(sub ProviderSubscription) ProviderSubscription {
			sub.ProviderID = ProviderIDAnthropic
			return sub
		},
		"blank access token": func(sub ProviderSubscription) ProviderSubscription {
			sub.AccessToken = " "
			return sub
		},
		"blank account id": func(sub ProviderSubscription) ProviderSubscription {
			sub.AccountID = " "
			return sub
		},
		"blank endpoint": func(sub ProviderSubscription) ProviderSubscription {
			sub.APIEndpointURL = " "
			return sub
		},
		"expired": func(sub ProviderSubscription) ProviderSubscription {
			sub.ExpiresAt = time.Now().Add(-time.Second)
			return sub
		},
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			ClearProviderSubscription(ProviderIDOpenAI)
			SetProviderSubscription(ProviderIDOpenAI, mutate(valid))

			_, ok := GetProviderSubscription(ProviderIDOpenAI)
			require.False(t, ok)
			require.False(t, ProviderHasSubscription(ProviderIDOpenAI))
		})
	}
}

func TestProviderSubscriptionRequiredSuppressesProviderKeyFallback(t *testing.T) {
	clearProviderAuthForTest(t)

	ConfigureProviderKey(ProviderIDOpenAI, "configured")
	t.Setenv("OPENAI_API_KEY", "default")
	require.True(t, ProviderHasConfiguredKey(ProviderIDOpenAI))
	require.Equal(t, "configured", GetAPIKey(DefaultModel))

	SetProviderSubscriptionRequired(ProviderIDOpenAI, true)
	require.True(t, ProviderSubscriptionRequired(ProviderIDOpenAI))
	require.Equal(t, "", GetAPIKey(DefaultModel))
	require.False(t, ModelUsesProviderSubscription(DefaultModel))
	require.NotContains(t, AvailableModelIDsWithAPIKey(), DefaultModel)
	require.NotContains(t, AvailableModelIDsWithAuth(), DefaultModel)

	expired := validProviderSubscription(ProviderIDOpenAI)
	expired.ExpiresAt = time.Now().Add(-time.Second)
	SetProviderSubscription(ProviderIDOpenAI, expired)
	require.False(t, ProviderHasSubscription(ProviderIDOpenAI))
	require.Equal(t, "", GetAPIKey(DefaultModel))
	require.NotContains(t, AvailableModelIDsWithAuth(), DefaultModel)

	SetProviderSubscription(ProviderIDOpenAI, validProviderSubscription(ProviderIDOpenAI))
	require.True(t, ModelUsesProviderSubscription(DefaultModel))
	require.Contains(t, AvailableModelIDsWithAuth(), DefaultModel)

	ClearProviderSubscription(ProviderIDOpenAI)
	SetProviderSubscriptionRequired(ProviderIDOpenAI, false)
	require.Equal(t, "configured", GetAPIKey(DefaultModel))
}

func TestProviderSubscriptionRequiredPreservesPerModelOverrides(t *testing.T) {
	clearProviderAuthForTest(t)

	ConfigureProviderKey(ProviderIDOpenAI, "configured")
	SetProviderSubscriptionRequired(ProviderIDOpenAI, true)

	actualKeyID := ModelID("custom-openai-required-actual-key")
	err := AddCustomModel(actualKeyID, ProviderIDOpenAI, "gpt-5.6-sol", ModelOverrides{APIActualKey: "literal"})
	require.NoError(t, err)

	envKeyID := ModelID("custom-openai-required-env-key")
	t.Setenv("CUSTOM_REQUIRED_OPENAI_API_KEY", "alt")
	err = AddCustomModel(envKeyID, ProviderIDOpenAI, "gpt-5.6-sol", ModelOverrides{APIEnvKey: "$CUSTOM_REQUIRED_OPENAI_API_KEY"})
	require.NoError(t, err)

	unsetEnvKeyID := ModelID("custom-openai-required-unset-env-key")
	t.Setenv("CUSTOM_REQUIRED_UNSET_OPENAI_API_KEY", "")
	err = AddCustomModel(unsetEnvKeyID, ProviderIDOpenAI, "gpt-5.6-sol", ModelOverrides{APIEnvKey: "$CUSTOM_REQUIRED_UNSET_OPENAI_API_KEY"})
	require.NoError(t, err)

	endpointID := ModelID("custom-openai-required-endpoint")
	err = AddCustomModel(endpointID, ProviderIDOpenAI, "gpt-5.6-sol", ModelOverrides{APIEndpointURL: "http://localhost:1234/v1"})
	require.NoError(t, err)

	require.Equal(t, "literal", GetAPIKey(actualKeyID))
	require.Equal(t, "alt", GetAPIKey(envKeyID))
	require.Equal(t, "", GetAPIKey(unsetEnvKeyID))
	require.Equal(t, "configured", GetAPIKey(endpointID))
	require.Equal(t, "http://localhost:1234/v1", GetAPIEndpointURL(endpointID))

	require.False(t, ModelUsesProviderSubscription(actualKeyID))
	require.False(t, ModelUsesProviderSubscription(envKeyID))
	require.False(t, ModelUsesProviderSubscription(unsetEnvKeyID))
	require.False(t, ModelUsesProviderSubscription(endpointID))

	apiKeyModels := AvailableModelIDsWithAPIKey()
	require.Contains(t, apiKeyModels, actualKeyID)
	require.Contains(t, apiKeyModels, envKeyID)
	require.NotContains(t, apiKeyModels, unsetEnvKeyID)
	require.Contains(t, apiKeyModels, endpointID)

	SetProviderSubscription(ProviderIDOpenAI, validProviderSubscription(ProviderIDOpenAI))
	require.True(t, ModelUsesProviderSubscription(DefaultModel))
	require.True(t, ModelUsesProviderSubscription(unsetEnvKeyID))
	require.False(t, ModelUsesProviderSubscription(endpointID))
}

func TestAvailableModelIDsWithAuthUsesSubscriptionEligibility(t *testing.T) {
	clearProviderAuthForTest(t)
	require.NotContains(t, AvailableModelIDsWithAuth(), DefaultModel)

	expired := validProviderSubscription(ProviderIDOpenAI)
	expired.ExpiresAt = time.Now().Add(-time.Second)
	SetProviderSubscription(ProviderIDOpenAI, expired)
	require.NotContains(t, AvailableModelIDsWithAuth(), DefaultModel)
	require.False(t, modelHasEligibleProviderSubscription(DefaultModel))

	SetProviderSubscription(ProviderIDOpenAI, validProviderSubscription(ProviderIDOpenAI))

	noOverrideID := ModelID("custom-openai-subscription-no-override")
	err := AddCustomModel(noOverrideID, ProviderIDOpenAI, "gpt-5.6-sol", ModelOverrides{})
	require.NoError(t, err)

	actualKeyID := ModelID("custom-openai-subscription-actual-key")
	err = AddCustomModel(actualKeyID, ProviderIDOpenAI, "gpt-5.6-sol", ModelOverrides{APIActualKey: "literal"})
	require.NoError(t, err)

	usableEnvKeyID := ModelID("custom-openai-subscription-env-key")
	t.Setenv("CUSTOM_SUBSCRIPTION_OPENAI_API_KEY", "alt")
	err = AddCustomModel(usableEnvKeyID, ProviderIDOpenAI, "gpt-5.6-sol", ModelOverrides{APIEnvKey: "$CUSTOM_SUBSCRIPTION_OPENAI_API_KEY"})
	require.NoError(t, err)

	unsetEnvKeyID := ModelID("custom-openai-subscription-unset-env-key")
	t.Setenv("CUSTOM_SUBSCRIPTION_UNSET_OPENAI_API_KEY", "")
	err = AddCustomModel(unsetEnvKeyID, ProviderIDOpenAI, "gpt-5.6-sol", ModelOverrides{APIEnvKey: "$CUSTOM_SUBSCRIPTION_UNSET_OPENAI_API_KEY"})
	require.NoError(t, err)

	endpointID := ModelID("custom-openai-subscription-endpoint")
	err = AddCustomModel(endpointID, ProviderIDOpenAI, "gpt-5.6-sol", ModelOverrides{APIEndpointURL: "http://localhost:1234/v1"})
	require.NoError(t, err)

	require.True(t, modelHasEligibleProviderSubscription(DefaultModel))
	require.True(t, modelHasEligibleProviderSubscription(noOverrideID))
	require.False(t, modelHasEligibleProviderSubscription(actualKeyID))
	require.False(t, modelHasEligibleProviderSubscription(usableEnvKeyID))
	require.True(t, modelHasEligibleProviderSubscription(unsetEnvKeyID))
	require.False(t, modelHasEligibleProviderSubscription(endpointID))
	require.True(t, ModelUsesProviderSubscription(DefaultModel))
	require.True(t, ModelUsesProviderSubscription(noOverrideID))
	require.False(t, ModelUsesProviderSubscription(actualKeyID))
	require.False(t, ModelUsesProviderSubscription(usableEnvKeyID))
	require.True(t, ModelUsesProviderSubscription(unsetEnvKeyID))
	require.False(t, ModelUsesProviderSubscription(endpointID))

	authModels := AvailableModelIDsWithAuth()
	require.Contains(t, authModels, DefaultModel)
	require.Contains(t, authModels, noOverrideID)
	require.Contains(t, authModels, actualKeyID)
	require.Contains(t, authModels, usableEnvKeyID)
	require.Contains(t, authModels, unsetEnvKeyID)
	require.NotContains(t, authModels, endpointID)

	require.NotContains(t, AvailableModelIDsWithAPIKey(), DefaultModel)
	require.Contains(t, AvailableModelIDsWithAPIKey(), actualKeyID)
	require.Contains(t, AvailableModelIDsWithAPIKey(), usableEnvKeyID)
	require.NotContains(t, AvailableModelIDsWithAPIKey(), unsetEnvKeyID)

	ConfigureProviderKey(ProviderIDOpenAI, "configured")
	t.Setenv("OPENAI_API_KEY", "default")
	require.True(t, ProviderHasSubscription(ProviderIDOpenAI))
	require.True(t, modelHasEligibleProviderSubscription(DefaultModel))
}
