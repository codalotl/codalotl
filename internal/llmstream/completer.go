package llmstream

import (
	"context"
	"errors"

	"github.com/codalotl/codalotl/internal/llmmodel"
)

// Completer provides one-shot completions.
type Completer interface {
	// Complete sends systemMessage and userMessage to modelID, returning the final assistant turn.
	Complete(ctx context.Context, modelID llmmodel.ModelID, systemMessage, userMessage string, options ...SendOptions) (Turn, error)
}

type completer struct {
	newConversation func(llmmodel.ModelID, string) StreamingConversation
}

// NewCompleter returns a Completer.
func NewCompleter() Completer {
	return completer{newConversation: NewConversation}
}

func (c completer) Complete(ctx context.Context, modelID llmmodel.ModelID, systemMessage, userMessage string, options ...SendOptions) (Turn, error) {
	newConversation := c.newConversation
	if newConversation == nil {
		newConversation = NewConversation
	}

	conv := newConversation(modelID, systemMessage)
	if err := conv.AddUserTurn(userMessage); err != nil {
		return Turn{}, err
	}

	var firstErr error
	for events := conv.SendAsync(ctx, options...); ; {
		select {
		case <-ctx.Done():
			if firstErr != nil {
				return Turn{}, firstErr
			}
			return Turn{}, ctx.Err()
		case ev, ok := <-events:
			if !ok {
				if firstErr != nil {
					return Turn{}, firstErr
				}
				if err := ctx.Err(); err != nil {
					return Turn{}, err
				}
				return Turn{}, errors.New("llmstream completion stream closed without successful completion")
			}

			if ev.Error != nil && firstErr == nil {
				firstErr = ev.Error
			}
			if ev.Type != EventTypeCompletedSuccess {
				continue
			}
			if ev.Turn == nil {
				if firstErr == nil {
					firstErr = errors.New("llmstream completion completed without a turn")
				}
				continue
			}
			return *ev.Turn, nil
		}
	}
}
