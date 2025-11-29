package llmcomplete

import (
	"fmt"
	"math"

	"github.com/tiktoken-go/tokenizer"
)

// CountTokensSpecificModel returns the token count for text using the tokenizer appropriate for the model. For Anthropic models, it applies a 1.2x multiplier to approximate their tokenizer
// behavior.
//
// NOTE: as of 2025/08/26, based on the models/providers we support (OpenAI - latest models, Grok3/4, Anthropic), we just use tokenizer.O200kBase for all models (with Anthropic adjustment).
func CountTokensSpecificModel(text string, modelID ModelID) int {
	enc, err := tokenizer.Get(tokenizer.O200kBase)
	if err != nil {
		panic(fmt.Errorf("invalid encoder: %v", tokenizer.O200kBase))
	}

	count, err := enc.Count(text)
	if err != nil {
		fmt.Printf("WARNING: could not count tokens for text. err= %v\n", err)
		return len(text) / 4
	}

	if modelOrDefault(modelID).providerID == ProviderIDAnthropic {
		return int(math.Round(float64(count) * 1.2))
	}

	return count
}
