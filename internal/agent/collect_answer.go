package agent

import (
	"context"
	"fmt"
)

// CollectFinalAssistantText drains an agent event stream and returns the final assistant text answer for the top-level agent, or an error if that agent does not
// terminate successfully.
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
			if event.AssistantTextFinal {
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

	if err := ctx.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("agent did not return an answer")
}
