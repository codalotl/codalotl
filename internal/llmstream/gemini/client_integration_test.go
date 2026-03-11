package gemini

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelsGenerateContentStream_IntegrationRealAPI_Text(t *testing.T) {
	if os.Getenv("INTEGRATION_TEST") == "" {
		t.Skip("INTEGRATION_TEST is required to run these tests")
	}

	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("GOOGLE_API_KEY")
	}
	if apiKey == "" {
		t.Skip("GEMINI_API_KEY or GOOGLE_API_KEY is required to run these tests")
	}

	model := os.Getenv("GEMINI_MODEL")
	if model == "" {
		model = "gemini-2.5-flash"
	}

	client, err := NewClient(context.Background(), &ClientConfig{APIKey: apiKey})
	require.NoError(t, err)

	marker := "PING-" + time.Now().UTC().Format("150405.000000000")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	stream := client.Models.GenerateContentStream(ctx, model, []*Content{{
		Role: string(RoleUser),
		Parts: []*Part{{
			Text: "Reply with exactly " + marker + " and no other text.",
		}},
	}}, &GenerateContentConfig{
		MaxOutputTokens: 32,
		Temperature:     Ptr(float32(0)),
		ThinkingConfig: &ThinkingConfig{
			IncludeThoughts: false,
			ThinkingBudget:  Ptr(int32(0)),
		},
	})

	var (
		textBuilder  strings.Builder
		finishReason FinishReason
		usage        *GenerateContentResponseUsageMetadata
		gotResponses int
	)
	for resp, err := range stream {
		require.NoError(t, err)
		gotResponses++
		if resp.UsageMetadata != nil {
			usage = resp.UsageMetadata
		}
		for _, candidate := range resp.Candidates {
			if candidate == nil {
				continue
			}
			if candidate.FinishReason != "" {
				finishReason = candidate.FinishReason
			}
			if candidate.Content == nil {
				continue
			}
			for _, part := range candidate.Content.Parts {
				if part != nil && part.Text != "" {
					textBuilder.WriteString(part.Text)
				}
			}
		}
	}

	assert.Greater(t, gotResponses, 0)
	assert.Contains(t, textBuilder.String(), marker)
	assert.Equal(t, FinishReasonStop, finishReason)
	require.NotNil(t, usage)
	assert.Greater(t, usage.PromptTokenCount, int32(0))
}

func TestModelsGenerateContentStream_IntegrationRealAPI_ToolCall(t *testing.T) {
	if os.Getenv("INTEGRATION_TEST") == "" {
		t.Skip("INTEGRATION_TEST is required to run these tests")
	}

	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("GOOGLE_API_KEY")
	}
	if apiKey == "" {
		t.Skip("GEMINI_API_KEY or GOOGLE_API_KEY is required to run these tests")
	}

	client, err := NewClient(context.Background(), &ClientConfig{APIKey: apiKey})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	stream := client.Models.GenerateContentStream(ctx, "gemini-2.5-flash", []*Content{{
		Role: string(RoleUser),
		Parts: []*Part{{
			Text: `Call the tool named get_weather exactly once with {"location":"San Francisco"}. Do not answer in natural language.`,
		}},
	}}, &GenerateContentConfig{
		MaxOutputTokens: 80,
		Temperature:     Ptr(float32(0)),
		Tools: []*Tool{{
			FunctionDeclarations: []*FunctionDeclaration{{
				Name:        "get_weather",
				Description: "Get weather",
				ParametersJsonSchema: map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"location": map[string]any{"type": "string"},
					},
					"required":         []string{"location"},
					"propertyOrdering": []string{"location"},
				},
			}},
		}},
		ThinkingConfig: &ThinkingConfig{
			IncludeThoughts: false,
			ThinkingBudget:  Ptr(int32(0)),
		},
	})

	var (
		call         *FunctionCall
		finishReason FinishReason
		iterErr      error
	)
	for resp, err := range stream {
		if err != nil {
			iterErr = err
			break
		}
		for _, candidate := range resp.Candidates {
			if candidate == nil {
				continue
			}
			if candidate.FinishReason != "" {
				finishReason = candidate.FinishReason
			}
			if candidate.Content == nil {
				continue
			}
			for _, part := range candidate.Content.Parts {
				if part != nil && part.FunctionCall != nil {
					call = part.FunctionCall
				}
			}
		}
	}

	require.NoError(t, iterErr)
	require.NotNil(t, call)
	assert.Equal(t, "get_weather", call.Name)
	assert.Equal(t, "San Francisco", call.Args["location"])
	assert.Equal(t, FinishReasonStop, finishReason)
	assert.False(t, errors.Is(iterErr, io.EOF))
}
