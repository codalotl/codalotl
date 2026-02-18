package reorgbot

import (
	"github.com/codalotl/codalotl/internal/llmcomplete"
	"log/slog"
	"strings"
)

// responsesConversationalist is copied from codeai/docubot/add_docs_test.go with minimal adjustments.
type responsesConversationalist struct {
	responses []string
	convs     []*interceptingConversation
}

type interceptingConversation struct {
	inner            llmcomplete.Conversation
	userMessagesText []string
}

func (o *responsesConversationalist) NewConversation(model llmcomplete.ModelID, systemMessage string) llmcomplete.Conversation {
	if len(o.responses) == 0 {
		panic("unexpected conversation; add more responses as needed")
	}
	resp := o.responses[0]
	o.responses = o.responses[1:]
	inner := llmcomplete.NewMockConversation(model, systemMessage, map[string]string{"": resp})
	ic := &interceptingConversation{inner: inner}
	o.convs = append(o.convs, ic)
	return ic
}

func (c *interceptingConversation) SetLogger(l *slog.Logger) { c.inner.SetLogger(l) }

func (c *interceptingConversation) AddUserMessage(msg string) *llmcomplete.Message {
	c.userMessagesText = append(c.userMessagesText, msg)
	return c.inner.AddUserMessage(msg)
}

func (c *interceptingConversation) Send() (*llmcomplete.Message, error) { return c.inner.Send() }

func (c *interceptingConversation) LastMessage() *llmcomplete.Message { return c.inner.LastMessage() }

func (c *interceptingConversation) Messages() []*llmcomplete.Message { return c.inner.Messages() }

func (c *interceptingConversation) LastError() *llmcomplete.ResponseError { return c.inner.LastError() }

func (c *interceptingConversation) Usage() []llmcomplete.Usage { return c.inner.Usage() }

func (o *responsesConversationalist) allUserText() string {
	var b strings.Builder
	for _, c := range o.convs {
		for _, msg := range c.userMessagesText {
			b.WriteString(msg)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// stagedConversationalist returns initial canned responses; subsequent conversations dynamically respond with a JSON array of ids extracted from the last user message
// (for ResortFile tests).
type stagedConversationalist struct {
	initial []string
	idx     int
}

func (s *stagedConversationalist) NewConversation(model llmcomplete.ModelID, systemMessage string) llmcomplete.Conversation {
	if s.idx < len(s.initial) {
		resp := s.initial[s.idx]
		s.idx++
		inner := llmcomplete.NewMockConversation(model, systemMessage, map[string]string{"": resp})
		return &interceptingConversation{inner: inner}
	}
	return &dynamicArrayConversation{msgs: []*llmcomplete.Message{{Role: llmcomplete.RoleSystem, Text: systemMessage}}}
}

// dynamicArrayConversation builds a JSON array of ids found in the user message (lines beginning with "// id: ") and returns that as the assistant response.
type dynamicArrayConversation struct {
	msgs   []*llmcomplete.Message
	logger *slog.Logger
}

func (c *dynamicArrayConversation) SetLogger(l *slog.Logger) { c.logger = l }
func (c *dynamicArrayConversation) AddUserMessage(msg string) *llmcomplete.Message {
	m := &llmcomplete.Message{Role: llmcomplete.RoleUser, Text: msg}
	c.msgs = append(c.msgs, m)
	return m
}
func (c *dynamicArrayConversation) Send() (*llmcomplete.Message, error) {
	last := c.LastMessage()
	if last == nil || last.Role != llmcomplete.RoleUser {
		return nil, nil
	}
	var ids []string
	for _, line := range strings.Split(last.Text, "\n") {
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
	m := &llmcomplete.Message{Role: llmcomplete.RoleAssistant, Text: b.String()}
	c.msgs = append(c.msgs, m)
	return m, nil
}
func (c *dynamicArrayConversation) LastMessage() *llmcomplete.Message {
	if len(c.msgs) == 0 {
		return nil
	}
	return c.msgs[len(c.msgs)-1]
}
func (c *dynamicArrayConversation) Messages() []*llmcomplete.Message      { return c.msgs }
func (c *dynamicArrayConversation) LastError() *llmcomplete.ResponseError { return nil }
func (c *dynamicArrayConversation) Usage() []llmcomplete.Usage            { return nil }
