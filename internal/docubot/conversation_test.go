package docubot

import (
	"context"
	"strings"

	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
)

// responsesCompleter returns canned responses in the order they are requested. It records completions for inspection when needed.
type responsesCompleter struct {
	responses []string
	convs     []*interceptingCompletion
}

// allUserText returns the concatenation of all user messages sent across every completion.
func (o *responsesCompleter) allUserText() string {
	var b strings.Builder
	for _, c := range o.convs {
		for _, msg := range c.userMessagesText {
			b.WriteString(msg)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// Complete implements llmstream.Completer by returning the next canned response.
func (o *responsesCompleter) Complete(_ context.Context, _ llmmodel.ModelID, systemMessage, userMessage string, _ ...llmstream.SendOptions) (llmstream.Turn, error) {
	if len(o.responses) == 0 {
		panic("unexpected completion; add more responses as needed")
	}

	// Pop the next canned response.
	resp := o.responses[0]
	o.responses = o.responses[1:]

	o.convs = append(o.convs, &interceptingCompletion{
		systemMessage:    systemMessage,
		userMessagesText: []string{userMessage},
	})

	return llmstream.Turn{
		Role: llmstream.RoleAssistant,
		Parts: []llmstream.ContentPart{
			llmstream.TextContent{Content: resp},
		},
		FinishReason: llmstream.FinishReasonEndTurn,
	}, nil
}

// interceptingCompletion records one completion request for inspection.
type interceptingCompletion struct {
	systemMessage    string
	userMessagesText []string
}
