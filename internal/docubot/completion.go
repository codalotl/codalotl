package docubot

import (
	"context"

	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
)

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

	turn, err := completer.Complete(options.completionContext(), llmmodel.ModelIDOrFallback(options.Model), systemMessage, userMessage)
	if err != nil {
		return "", err
	}
	return turn.TextContent(), nil
}
