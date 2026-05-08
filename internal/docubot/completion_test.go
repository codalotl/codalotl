package docubot

import (
	"context"
	"errors"
	"testing"

	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type contextErrCompleter struct{}

func (contextErrCompleter) Complete(ctx context.Context, _ llmmodel.ModelID, _, _ string, _ ...llmstream.SendOptions) (llmstream.Turn, error) {
	return llmstream.Turn{}, ctx.Err()
}

type staticCompleter struct {
	turn llmstream.Turn
	err  error
	ctx  context.Context
}

func (c *staticCompleter) Complete(ctx context.Context, _ llmmodel.ModelID, _, _ string, _ ...llmstream.SendOptions) (llmstream.Turn, error) {
	c.ctx = ctx
	return c.turn, c.err
}

type completionContextTestKey struct{}

func TestCompleteText_UsesOptionsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := completeText("system", "user", BaseOptions{
		Context:   ctx,
		Completer: contextErrCompleter{},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestCompleteText_EmitsExternalLLMUsageOnSuccess(t *testing.T) {
	ctx := context.WithValue(context.Background(), completionContextTestKey{}, "ctx")
	usage := llmstream.TokenUsage{
		TotalInputTokens:         10,
		CachedInputTokens:        2,
		CacheCreationInputTokens: 3,
		ReasoningTokens:          4,
		TotalOutputTokens:        5,
	}
	completer := &staticCompleter{
		turn: llmstream.Turn{
			Parts: []llmstream.ContentPart{llmstream.TextContent{Content: "done"}},
			Usage: usage,
		},
	}

	oldEmit := emitExternalLLMUsage
	defer func() {
		emitExternalLLMUsage = oldEmit
	}()

	var calls int
	var emittedCtx context.Context
	var emittedUsage llmstream.TokenUsage
	emitExternalLLMUsage = func(ctx context.Context, usage llmstream.TokenUsage) {
		calls++
		emittedCtx = ctx
		emittedUsage = usage
	}

	text, err := completeText("system", "user", BaseOptions{
		Context:   ctx,
		Completer: completer,
	})
	require.NoError(t, err)

	assert.Equal(t, "done", text)
	assert.Equal(t, "ctx", completer.ctx.Value(completionContextTestKey{}))
	assert.Equal(t, 1, calls)
	assert.Equal(t, "ctx", emittedCtx.Value(completionContextTestKey{}))
	assert.Equal(t, usage, emittedUsage)
}

func TestCompleteText_DoesNotEmitExternalLLMUsageOnError(t *testing.T) {
	oldEmit := emitExternalLLMUsage
	defer func() {
		emitExternalLLMUsage = oldEmit
	}()

	var calls int
	emitExternalLLMUsage = func(context.Context, llmstream.TokenUsage) {
		calls++
	}

	_, err := completeText("system", "user", BaseOptions{
		Completer: &staticCompleter{err: errors.New("boom")},
	})
	require.Error(t, err)
	assert.Equal(t, 0, calls)
}

func TestCompleteText_SucceedsOutsideAgentContext(t *testing.T) {
	text, err := completeText("system", "user", BaseOptions{
		Completer: &staticCompleter{
			turn: llmstream.Turn{
				Parts: []llmstream.ContentPart{llmstream.TextContent{Content: "done"}},
				Usage: llmstream.TokenUsage{
					TotalInputTokens:  10,
					TotalOutputTokens: 5,
				},
			},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "done", text)
}
