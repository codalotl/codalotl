package agent

import (
	"context"
	"fmt"
)

// CollectFinalAssistantText drains an agent event stream and returns the final assistant text answer, or an error if the stream terminates unsuccessfully.
func CollectFinalAssistantText(ctx context.Context, events <-chan Event) (string, error) {
	finalAssistantText := ""
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
			if event.AssistantTextFinalizing {
				finalAssistantText = event.TextContent.Content
			}
		case EventTypeDoneSuccess:
			return finalAssistantText, nil
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

	if finalAssistantText != "" {
		return finalAssistantText, nil
	}

	if err := ctx.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("agent did not return an answer")
}
