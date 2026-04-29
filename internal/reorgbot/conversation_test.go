package reorgbot

import (
	"context"
	"strings"
	"sync"

	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
)

type responsesCompleter struct {
	mu        sync.Mutex
	responses []string
	convs     []*interceptingCompletion
}

type interceptingCompletion struct {
	systemMessage    string
	userMessagesText []string
}

func (o *responsesCompleter) Complete(_ context.Context, _ llmmodel.ModelID, systemMessage, userMessage string, _ ...llmstream.SendOptions) (llmstream.Turn, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if len(o.responses) == 0 {
		panic("unexpected completion; add more responses as needed")
	}

	resp := o.responses[0]
	o.responses = o.responses[1:]

	ic := &interceptingCompletion{
		systemMessage:    systemMessage,
		userMessagesText: []string{userMessage},
	}
	o.convs = append(o.convs, ic)

	return llmstream.Turn{
		Role: llmstream.RoleAssistant,
		Parts: []llmstream.ContentPart{
			llmstream.TextContent{Content: resp},
		},
		FinishReason: llmstream.FinishReasonEndTurn,
	}, nil
}

func (o *responsesCompleter) allUserText() string {
	o.mu.Lock()
	defer o.mu.Unlock()

	var b strings.Builder
	for _, c := range o.convs {
		for _, msg := range c.userMessagesText {
			b.WriteString(msg)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// stagedCompleter returns initial canned responses; subsequent requests dynamically respond with a JSON array of ids extracted from the user message (for ResortFile
// tests).
type stagedCompleter struct {
	mu      sync.Mutex
	initial []string
	idx     int
}

func (s *stagedCompleter) Complete(_ context.Context, _ llmmodel.ModelID, _ string, userMessage string, _ ...llmstream.SendOptions) (llmstream.Turn, error) {
	s.mu.Lock()
	if s.idx < len(s.initial) {
		resp := s.initial[s.idx]
		s.idx++
		s.mu.Unlock()
		return llmstream.Turn{
			Role: llmstream.RoleAssistant,
			Parts: []llmstream.ContentPart{
				llmstream.TextContent{Content: resp},
			},
			FinishReason: llmstream.FinishReasonEndTurn,
		}, nil
	}
	s.mu.Unlock()

	var ids []string
	for _, line := range strings.Split(userMessage, "\n") {
		l := strings.TrimSpace(line)
		const p = "// id: "
		if strings.HasPrefix(l, p) {
			ids = append(ids, strings.TrimSpace(l[len(p):]))
		}
	}
	var b strings.Builder
	b.WriteString("[")
	for i, id := range ids {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString("\"")
		b.WriteString(id)
		b.WriteString("\"")
	}
	b.WriteString("]")
	return llmstream.Turn{
		Role: llmstream.RoleAssistant,
		Parts: []llmstream.ContentPart{
			llmstream.TextContent{Content: b.String()},
		},
		FinishReason: llmstream.FinishReasonEndTurn,
	}, nil
}
