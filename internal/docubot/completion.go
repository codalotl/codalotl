package docubot

import (
	"context"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
)

var emitExternalLLMUsage = agent.EmitExternalLLMUsage

// completionContext returns the context used for LLM completion calls, defaulting to context.Background.
func (o BaseOptions) completionContext() context.Context {
	if o.Context != nil {
		return o.Context
	}
	return context.Background()
}

func completeText(systemMessage, userMessage string, options BaseOptions) (string, error) {
	completer := options.Completer
	if completer == nil {
		completer = llmstream.NewCompleter()
	}

	ctx := options.completionContext()
	turn, err := completer.Complete(ctx, llmmodel.ModelIDOrFallback(options.Model), systemMessage, userMessage)
	if err != nil {
		return "", err
	}
	emitExternalLLMUsage(ctx, turn.Usage)
	return turn.TextContent(), nil
}
