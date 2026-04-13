package agent

import (
	"context"
	"fmt"
	"strings"
)

// CollectFinalAssistantText drains an agent event stream and returns the final assistant text answer, or an error if the stream terminates unsuccessfully.
func CollectFinalAssistantText(ctx context.Context, events <-chan Event) (string, error) {
	var assistantText []string
	lastTurnText := ""
	targetAgentID := ""

	for event := range events {
		if event.Agent.ID != "" {
			if targetAgentID == "" {
				targetAgentID = event.Agent.ID
			}
			if event.Agent.ID != targetAgentID {
				continue
			}
		}

		switch event.Type {
		case EventTypeAssistantText:
			text := strings.TrimSpace(event.TextContent.Content)
			if text != "" {
				assistantText = append(assistantText, text)
			}
		case EventTypeAssistantTurnComplete:
			if event.Turn != nil {
				lastTurnText = strings.TrimSpace(event.Turn.TextContent())
			}
		case EventTypeDoneSuccess:
			if lastTurnText != "" {
				return lastTurnText, nil
			}
			if len(assistantText) > 0 {
				return strings.Join(assistantText, "\n\n"), nil
			}
			return "", nil
		case EventTypeCanceled:
			if event.Error != nil {
				return "", event.Error
			}
			if err := ctx.Err(); err != nil {
				return "", err
			}
			return "", context.Canceled
		case EventTypeError:
			if event.Error != nil {
				return "", event.Error
			}
			return "", fmt.Errorf("agent failed")
		}
	}

	if lastTurnText != "" {
		return lastTurnText, nil
	}
	if len(assistantText) > 0 {
		return strings.Join(assistantText, "\n\n"), nil
	}

	if err := ctx.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("agent did not return an answer")
}
