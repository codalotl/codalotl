package tui

import (
	"encoding/json"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/llmstream"
)

type fakePlanItem struct {
	Step   string `json:"step"`
	Status string `json:"status"`
}

func fakeAgentEvents() []agent.Event {
	meta := agent.AgentMeta{ID: tuiAgentName}
	events := []agent.Event{
		{
			Agent: meta,
			Type:  agent.EventTypeAssistantReasoning,
			ReasoningContent: llmstream.ReasoningContent{
				ProviderID: "fake",
				Content:    "Sketching a fake run: map the work, peek at files, and wrap up with a short note.",
			},
		},
	}

	events = append(events, fakeUpdatePlanEvents(meta, "fake-plan-1", "Kick off with a scoped plan.", []fakePlanItem{
		{Step: "Outline tasks to demo", Status: "in_progress"},
		{Step: "Preview code snippets", Status: "pending"},
	})...)
	events = append(events, fakeToolEvents(meta, "read_file", "fake-read-1",
		struct {
			Path string `json:"path"`
		}{Path: "README.md"},
		struct {
			Content string `json:"content"`
		}{Content: "Fake README header\nNotes about the workspace\n\nSample content for display."},
		false,
	)...)
	events = append(events, fakeToolEvents(meta, "ls", "fake-ls-1",
		struct {
			Path string `json:"path"`
		}{Path: "codeai/tui"},
		struct {
			Content string `json:"content"`
		}{Content: "tui.go\nsession.go\npalette.go\ndebug.go"},
		false,
	)...)
	events = append(events, fakeUpdatePlanEvents(meta, "fake-plan-2", "Refining the plan after file scans.", []fakePlanItem{
		{Step: "Outline tasks to demo", Status: "completed"},
		{Step: "Preview code snippets", Status: "in_progress"},
		{Step: "Prepare patch preview", Status: "pending"},
	})...)
	events = append(events, fakeToolEvents(meta, "apply_patch", "fake-patch-1",
		fakePatchInput(),
		struct {
			Success bool `json:"success"`
		}{Success: true},
		false,
	)...)
	events = append(events, fakeUpdatePlanEvents(meta, "fake-plan-3", "Wrap with the last status update.", []fakePlanItem{
		{Step: "Outline tasks to demo", Status: "completed"},
		{Step: "Preview code snippets", Status: "completed"},
		{Step: "Prepare patch preview", Status: "completed"},
	})...)

	events = append(events,
		agent.Event{
			Agent: meta,
			Type:  agent.EventTypeAssistantText,
			TextContent: llmstream.TextContent{
				ProviderID: "fake",
				Content:    "Fake stream complete. Everything above is simulated.",
			},
		},
		agent.Event{
			Agent: meta,
			Type:  agent.EventTypeDoneSuccess,
		},
	)

	return events
}

func fakeUpdatePlanEvents(meta agent.AgentMeta, callID, explanation string, plan []fakePlanItem) []agent.Event {
	payload := struct {
		Explanation string         `json:"explanation"`
		Plan        []fakePlanItem `json:"plan"`
	}{
		Explanation: explanation,
		Plan:        plan,
	}
	input := encodeFakePayload(payload)
	call := fakeToolCall("update_plan", callID, input)

	return []agent.Event{
		{
			Agent:    meta,
			Type:     agent.EventTypeToolCall,
			Tool:     "update_plan",
			ToolCall: call,
		},
		{
			Agent:    meta,
			Type:     agent.EventTypeToolComplete,
			Tool:     "update_plan",
			ToolCall: call,
			ToolResult: &llmstream.ToolResult{
				CallID:  callID,
				Name:    "update_plan",
				Type:    call.Type,
				Result:  `{"success":true}`,
				IsError: false,
			},
		},
	}
}

func fakeToolEvents(meta agent.AgentMeta, name, callID string, inputPayload any, resultPayload any, isError bool) []agent.Event {
	input := encodeFakePayload(inputPayload)
	result := encodeFakePayload(resultPayload)
	call := fakeToolCall(name, callID, input)

	return []agent.Event{
		{
			Agent:    meta,
			Type:     agent.EventTypeToolCall,
			Tool:     name,
			ToolCall: call,
		},
		{
			Agent:    meta,
			Type:     agent.EventTypeToolComplete,
			Tool:     name,
			ToolCall: call,
			ToolResult: &llmstream.ToolResult{
				CallID:  callID,
				Name:    name,
				Type:    call.Type,
				Result:  result,
				IsError: isError,
			},
		},
	}
}

func fakeToolCall(name, callID, input string) *llmstream.ToolCall {
	return &llmstream.ToolCall{
		ProviderID: "fake-provider",
		CallID:     callID,
		Name:       name,
		Type:       "function",
		Input:      input,
	}
}

func fakePatchInput() string {
	return `*** Begin Patch
*** Update File: demo/fake_run.txt
@@
-Old placeholder
+New placeholder content
+Another line for the preview
*** End Patch
`
}

func encodeFakePayload(payload any) string {
	if payload == nil {
		return ""
	}
	if s, ok := payload.(string); ok {
		return s
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return string(data)
}
