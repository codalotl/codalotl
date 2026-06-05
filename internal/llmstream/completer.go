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

// completer is the default Completer implementation.
type completer struct {
	// The conversation factory creates new conversations; nil falls back to NewConversation.
	newConversation func(llmmodel.ModelID, string) StreamingConversation
}

// NewCompleter returns a Completer.
func NewCompleter() Completer {
	return completer{newConversation: NewConversation}
}

// Complete performs a single user-message completion and returns the successful assistant turn.
//
// It creates a conversation with systemMessage, appends userMessage, sends it with options, and consumes stream events until completion, cancellation, or error.
func (c completer) Complete(ctx context.Context, modelID llmmodel.ModelID, systemMessage, userMessage string, options ...SendOptions) (Turn, error) {
	newConversation := c.newConversation
	if newConversation == nil {
		newConversation = NewConversation
	}

	conv := newConversation(modelID, systemMessage)
	if err := conv.AddUserTurn(userMessage); err != nil {
		return Turn{}, err
	}

	var retryErr error
	for events := conv.SendAsync(ctx, options...); ; {
		select {
		case <-ctx.Done():
			return Turn{}, ctx.Err()
		case ev, ok := <-events:
			if !ok {
				if retryErr != nil {
					return Turn{}, retryErr
				}
				if err := ctx.Err(); err != nil {
					return Turn{}, err
				}
				return Turn{}, errors.New("llmstream completion stream closed without successful completion")
			}

			if ev.Type == EventTypeError {
				if ev.Error == nil {
					return Turn{}, errors.New("llmstream completion stream emitted an error event without an error")
				}
				return Turn{}, ev.Error
			}
			if ev.Type == EventTypeRetry && ev.Error != nil {
				retryErr = ev.Error
			}
			if ev.Type != EventTypeCompletedSuccess {
				continue
			}
			if ev.Turn == nil {
				return Turn{}, errors.New("llmstream completion completed without a turn")
			}
			return *ev.Turn, nil
		}
	}
}
