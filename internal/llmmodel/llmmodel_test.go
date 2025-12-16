package llmmodel

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

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
	gpt5 := ModelID("gpt-5.2")
	claude := ModelID("claude-sonnet-4-5")
	gemini := ModelID("gemini-2.5-pro")
	grok := ModelID("grok-4")

	require.True(t, gpt5.Valid())
	require.True(t, claude.Valid())
	require.True(t, gemini.Valid())
	require.True(t, grok.Valid())

	claudeInfo := GetModelInfo(claude)
	require.Equal(t, ProviderIDAnthropic, claudeInfo.ProviderID)
	require.Equal(t, "claude-sonnet-4-5-20250929", claudeInfo.ProviderModelID)
	require.True(t, claudeInfo.IsDefault)
	require.Equal(t, ProviderIDAnthropic, claude.ProviderID())
	require.Equal(t, []ProviderAPIType{ProviderTypeAnthropic}, claudeInfo.SupportedTypes)

	gptInfo := GetModelInfo(gpt5)
	require.Equal(t, "high", gptInfo.ReasoningEffort)
	require.True(t, gptInfo.IsDefault)
	require.Equal(t, gpt5, ProviderIDOpenAI.DefaultModel())
	require.Equal(t, []ProviderAPIType{ProviderTypeOpenAIResponses, ProviderTypeOpenAICompletions}, gptInfo.SupportedTypes)

	require.False(t, ModelID("gpt-5-codex").Valid())
	require.False(t, ModelID("gpt-5.1-codex").Valid())
	codexMinimal := ModelID("gpt-5.1-codex-minimal")
	require.True(t, codexMinimal.Valid())
	codexMinimalInfo := GetModelInfo(codexMinimal)
	require.Equal(t, ProviderIDOpenAI, codexMinimalInfo.ProviderID)
	require.Equal(t, "gpt-5.1-codex", codexMinimalInfo.ProviderModelID)
	require.Equal(t, []ProviderAPIType{ProviderTypeOpenAIResponses}, codexMinimalInfo.SupportedTypes)
	require.Equal(t, "minimal", codexMinimalInfo.ReasoningEffort)
	codexHigh := ModelID("gpt-5.1-codex-high")
	require.True(t, codexHigh.Valid())
	require.Equal(t, "high", GetModelInfo(codexHigh).ReasoningEffort)

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
