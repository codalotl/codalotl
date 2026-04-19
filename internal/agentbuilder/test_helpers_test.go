package agentbuilder

import (
	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/llmstream"
)

func successfulSubagentEvents(answer string, leading ...agent.Event) []agent.Event {
	events := append([]agent.Event(nil), leading...)
	events = append(events,
		agent.Event{
			Type: agent.EventTypeAssistantTurnComplete,
			Turn: &llmstream.Turn{
				Role:  llmstream.RoleAssistant,
				Parts: []llmstream.ContentPart{llmstream.TextContent{Content: answer}},
			},
		},
		agent.Event{
			Type:               agent.EventTypeAssistantText,
			TextContent:        llmstream.TextContent{Content: answer},
			AssistantTextFinal: true,
		},
		agent.Event{Type: agent.EventTypeDoneSuccess},
	)
	return events
}
