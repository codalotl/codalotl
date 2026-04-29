package docubot

import (
	"context"

	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
)

func completeText(systemMessage, userMessage string, options BaseOptions) (string, error) {
	completer := options.Completer
	if completer == nil {
		completer = llmstream.NewCompleter()
	}

	turn, err := completer.Complete(context.Background(), llmmodel.ModelIDOrFallback(options.Model), systemMessage, userMessage)
	if err != nil {
		return "", err
	}
	return turn.TextContent(), nil
}
