package llmstream

import (
	"testing"

	"github.com/codalotl/codalotl/internal/llmmodel"

	"github.com/openai/openai-go/v3/responses"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAIResponsesApplySendOptions_ServiceTier(t *testing.T) {
	t.Run("model default sets service tier (nil options)", func(t *testing.T) {
		var params responses.ResponseNewParams
		err := openAIResponsesApplySendOptions(&params, llmmodel.ModelInfo{ModelOverrides: llmmodel.ModelOverrides{ServiceTier: "priority"}}, nil)
		require.NoError(t, err)
		assert.Equal(t, responses.ResponseNewParamsServiceTier("priority"), params.ServiceTier)
	})

	t.Run("model default sets service tier (empty options)", func(t *testing.T) {
		var params responses.ResponseNewParams
		err := openAIResponsesApplySendOptions(&params, llmmodel.ModelInfo{ModelOverrides: llmmodel.ModelOverrides{ServiceTier: "priority"}}, &SendOptions{})
		require.NoError(t, err)
		assert.Equal(t, responses.ResponseNewParamsServiceTier("priority"), params.ServiceTier)
	})

	t.Run("auto clears model default", func(t *testing.T) {
		var params responses.ResponseNewParams
		err := openAIResponsesApplySendOptions(&params, llmmodel.ModelInfo{ModelOverrides: llmmodel.ModelOverrides{ServiceTier: "priority"}}, &SendOptions{ServiceTier: "auto"})
		require.NoError(t, err)
		assert.Equal(t, responses.ResponseNewParamsServiceTier(""), params.ServiceTier)
	})

	t.Run("option overrides model default", func(t *testing.T) {
		var params responses.ResponseNewParams
		err := openAIResponsesApplySendOptions(&params, llmmodel.ModelInfo{ModelOverrides: llmmodel.ModelOverrides{ServiceTier: "priority"}}, &SendOptions{ServiceTier: "flex"})
		require.NoError(t, err)
		assert.Equal(t, responses.ResponseNewParamsServiceTier("flex"), params.ServiceTier)
	})

	t.Run("priority sets service tier", func(t *testing.T) {
		var params responses.ResponseNewParams
		err := openAIResponsesApplySendOptions(&params, llmmodel.ModelInfo{}, &SendOptions{ServiceTier: "priority"})
		require.NoError(t, err)
		assert.Equal(t, responses.ResponseNewParamsServiceTier("priority"), params.ServiceTier)
	})

	t.Run("flex sets service tier", func(t *testing.T) {
		var params responses.ResponseNewParams
		err := openAIResponsesApplySendOptions(&params, llmmodel.ModelInfo{}, &SendOptions{ServiceTier: "flex"})
		require.NoError(t, err)
		assert.Equal(t, responses.ResponseNewParamsServiceTier("flex"), params.ServiceTier)
	})

	t.Run("auto does not set service tier", func(t *testing.T) {
		var params responses.ResponseNewParams
		err := openAIResponsesApplySendOptions(&params, llmmodel.ModelInfo{}, &SendOptions{ServiceTier: "auto"})
		require.NoError(t, err)
		assert.Equal(t, responses.ResponseNewParamsServiceTier(""), params.ServiceTier)
	})

	t.Run("empty does not set service tier", func(t *testing.T) {
		var params responses.ResponseNewParams
		err := openAIResponsesApplySendOptions(&params, llmmodel.ModelInfo{}, &SendOptions{})
		require.NoError(t, err)
		assert.Equal(t, responses.ResponseNewParamsServiceTier(""), params.ServiceTier)
	})

	t.Run("invalid value errors", func(t *testing.T) {
		var params responses.ResponseNewParams
		err := openAIResponsesApplySendOptions(&params, llmmodel.ModelInfo{}, &SendOptions{ServiceTier: "enterprise"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid service tier")
	})

	t.Run("invalid model value errors (nil options)", func(t *testing.T) {
		var params responses.ResponseNewParams
		err := openAIResponsesApplySendOptions(&params, llmmodel.ModelInfo{ModelOverrides: llmmodel.ModelOverrides{ServiceTier: "enterprise"}}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid service tier")
	})
}
