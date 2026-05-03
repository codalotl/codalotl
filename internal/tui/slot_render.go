package tui

import (
	"strings"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/agentformatter"
	"github.com/codalotl/codalotl/internal/q/termformat"
	"github.com/codalotl/codalotl/internal/q/tui/tuicontrols"
)

type toolSubagentDisplay struct {
	messageIndex  int
	slots         []toolSubagentSlot
	slotIndexByID map[string]int
}

type toolSubagentSlot struct {
	agentID    string
	label      string
	done       bool
	stateKind  toolSubagentSlotStateKind
	stateEvent agent.Event
	stateText  string
}

type toolSubagentSlotStateKind int

const (
	toolSubagentSlotStateStarting toolSubagentSlotStateKind = iota
	toolSubagentSlotStateLiveEvent
	toolSubagentSlotStateTerminalEvent
	toolSubagentSlotStateTerminalText
)

func (m *model) startToolSubagentDisplay(ev agent.Event) {
	if ev.Type != agent.EventTypeToolCall || ev.ToolCall == nil {
		return
	}
	callID := ev.ToolCall.CallID
	if callID == "" {
		return
	}
	if _, ok := m.toolSubagentDisplays[callID]; ok {
		return
	}
	m.toolSubagentDisplays[callID] = &toolSubagentDisplay{
		messageIndex:  len(m.messages),
		slotIndexByID: make(map[string]int),
	}
}

func (m *model) handleToolSubagentDescendantEvent(ev agent.Event, autoScroll bool) bool {
	scopeRef, ok := m.owningToolDisplayScope(ev.Agent)
	if !ok {
		return false
	}
	scope := m.toolDisplayScope(scopeRef)
	if scope == nil {
		return false
	}
	display := m.toolSubagentDisplays[scope.call.CallID]
	if display == nil {
		return false
	}
	directAgentID, ok := m.toolScopeDirectChildAgentID(scopeRef.agentID, ev.Agent.ID)
	if !ok {
		return false
	}

	switch ev.Type {
	case agent.EventTypeStartSubagent:
		if directAgentID != ev.Agent.ID {
			return display.slot(directAgentID) != nil
		}

		label := strings.TrimSpace(ev.StartSubagent.Label)
		if label == "" {
			return false
		}

		slot := display.ensureSlot(directAgentID, label)
		updated := slot.label != label || slot.stateKind != toolSubagentSlotStateStarting
		slot.label = label
		slot.stateKind = toolSubagentSlotStateStarting
		slot.stateEvent = agent.Event{}
		slot.stateText = ""
		slot.done = false
		if updated {
			m.invalidateMessage(display.messageIndex)
			m.refreshViewport(autoScroll)
		}
		return true
	}

	slot := display.slot(directAgentID)
	if slot == nil {
		return false
	}

	updated := false
	switch ev.Type {
	case agent.EventTypeAssistantTurnComplete, agent.EventTypeDoneSuccess:
		if directAgentID == ev.Agent.ID && ev.Type == agent.EventTypeDoneSuccess && !slot.done {
			slot.stateKind = toolSubagentSlotStateTerminalText
			slot.stateText = "No final result"
			slot.done = true
			updated = true
		}
	case agent.EventTypeAssistantText:
		if ev.AssistantTextFinalizing {
			if ref, ok := m.enclosingToolDisplayScope(ev.Agent); ok {
				if presenterScope := m.toolDisplayScope(ref); presenterScope != nil && presenterScope.finalMessagePresenter != nil {
					slot.stateKind = toolSubagentSlotStateTerminalText
					slot.stateText = m.formatToolSubagentFinalText(presenterScope, m.subagentLabels[ev.Agent.ID], ev.TextContent.Content)
					slot.done = true
					updated = true
					break
				}
			}
		}
		if directAgentID == ev.Agent.ID && ev.AssistantTextFinalizing {
			slot.stateKind = toolSubagentSlotStateTerminalText
			slot.stateText = m.formatToolSubagentFinalText(scope, slot.label, ev.TextContent.Content)
			slot.done = true
			updated = true
			break
		}
		if !slot.done {
			slot.stateKind = toolSubagentSlotStateLiveEvent
			slot.stateEvent = m.normalizeToolSubagentSlotEvent(ev)
			updated = true
		}
	case agent.EventTypeError, agent.EventTypeCanceled:
		if directAgentID == ev.Agent.ID && !slot.done {
			slot.stateKind = toolSubagentSlotStateTerminalEvent
			slot.stateEvent = m.normalizeToolSubagentSlotEvent(ev)
			slot.done = true
			updated = true
			break
		}
		if !slot.done {
			slot.stateKind = toolSubagentSlotStateLiveEvent
			slot.stateEvent = m.normalizeToolSubagentSlotEvent(ev)
			updated = true
		}
	default:
		if !slot.done {
			slot.stateKind = toolSubagentSlotStateLiveEvent
			slot.stateEvent = m.normalizeToolSubagentSlotEvent(ev)
			updated = true
		}
	}
	if updated {
		m.invalidateMessage(display.messageIndex)
		m.refreshViewport(autoScroll)
	}
	return true
}

func (m *model) renderToolSpecificMessage(msg *chatMessage, width int) (string, bool) {
	if msg == nil || msg.kind != messageKindAgent {
		return "", false
	}
	switch msg.event.Type {
	case agent.EventTypeToolCall:
	case agent.EventTypeToolComplete:
	default:
		return "", false
	}
	if msg.toolSubagentDisplay == nil && len(msg.toolOutputs) == 0 {
		return "", false
	}
	return m.renderToolMessageWithDetails(msg, width), true
}

func (m *model) renderToolMessageWithDetails(msg *chatMessage, width int) string {
	if msg == nil {
		return ""
	}
	display := msg.toolSubagentDisplay
	if (display == nil || len(display.slots) == 0) && len(msg.toolOutputs) == 0 {
		return m.agentFormatter.FormatEvent(msg.event, width)
	}
	sections := []string{m.agentFormatter.FormatEvent(msg.event, width)}
	if output := m.renderToolOutputs(msg.toolOutputs, width); output != "" {
		sections = append(sections, output)
	}
	if slots := m.renderToolSubagentSlots(display, width); slots != "" {
		sections = append(sections, slots)
	}
	return strings.Join(sections, "\n")
}

func (m *model) renderToolOutputs(outputs []agent.Event, width int) string {
	if len(outputs) == 0 {
		return ""
	}
	sections := make([]string, 0, len(outputs))
	for _, ev := range outputs {
		rendered := strings.TrimRight(m.agentFormatter.FormatEvent(ev, width), "\n")
		if rendered == "" {
			continue
		}
		sections = append(sections, rendered)
	}
	return strings.Join(sections, "\n")
}

func (m *model) renderToolSubagentSlots(display *toolSubagentDisplay, width int) string {
	if display == nil || len(display.slots) == 0 {
		return ""
	}
	sections := make([]string, 0, len(display.slots))
	for i := range display.slots {
		sections = append(sections, m.renderToolSubagentSlot(display.slots[i], width))
	}
	return strings.Join(sections, "\n")
}

func (m *model) renderToolSubagentSlot(slot toolSubagentSlot, width int) string {
	label := strings.TrimSpace(slot.label)
	if label == "" {
		return ""
	}
	header := m.styleToolSubagentLine("  • " + termformatSanitizeLine(label))
	body := m.renderToolSubagentSlotBody(slot, width)
	if body == "" {
		return header
	}
	return header + "\n" + body
}

func (m *model) renderToolSubagentSlotBody(slot toolSubagentSlot, width int) string {
	innerWidth := max(width-4, 1)
	switch slot.stateKind {
	case toolSubagentSlotStateTerminalText:
		return m.indentToolSubagentBlock(m.renderToolSubagentTextBlock(slot.stateText, innerWidth), "    ")
	case toolSubagentSlotStateLiveEvent, toolSubagentSlotStateTerminalEvent:
		return m.indentToolSubagentBlock(m.agentFormatter.FormatEvent(slot.stateEvent, innerWidth), "    ")
	default:
		return m.indentToolSubagentBlock(m.renderToolSubagentTextBlock("Starting", innerWidth), "    ")
	}
}

func (m *model) formatToolSubagentFinalText(scope *toolDisplayScope, label string, finalMessage string) string {
	if scope != nil && scope.finalMessagePresenter != nil {
		block := scope.finalMessagePresenter.SubagentFinalMessage(scope.call, label, finalMessage)
		text := strings.TrimSpace(agentformatter.RenderPlainTextBlock(block))
		if text != "" {
			return text
		}
	}
	return strings.TrimSpace(finalMessage)
}

func (m *model) normalizeToolSubagentSlotEvent(ev agent.Event) agent.Event {
	ev.Agent.Depth = 0
	return ev
}

func (m *model) owningToolDisplayScope(meta agent.AgentMeta) (toolDisplayScopeRef, bool) {
	nearest, ok := m.enclosingToolDisplayScope(meta)
	if !ok {
		return toolDisplayScopeRef{}, false
	}

	var owner toolDisplayScopeRef
	hasOwner := false
	for agentID := meta.Parent; agentID != ""; agentID = m.agentParents[agentID] {
		scopes := m.activeToolScopes[agentID]
		for i := len(scopes) - 1; i >= 0; i-- {
			display := m.toolSubagentDisplays[scopes[i].call.CallID]
			if display == nil || len(display.slots) == 0 {
				continue
			}
			owner = toolDisplayScopeRef{agentID: agentID, index: i}
			hasOwner = true
		}
	}
	if hasOwner {
		return owner, true
	}
	return nearest, true
}

func (m *model) toolScopeDirectChildAgentID(scopeAgentID string, agentID string) (string, bool) {
	for current := agentID; current != ""; current = m.agentParents[current] {
		if m.agentParents[current] == scopeAgentID {
			return current, true
		}
	}
	return "", false
}

func (m *model) invalidateMessage(index int) {
	if index < 0 || index >= len(m.messages) {
		return
	}
	m.messages[index].formatted = ""
	m.messages[index].formattedWidth = 0
}

func (d *toolSubagentDisplay) slot(agentID string) *toolSubagentSlot {
	if d == nil {
		return nil
	}
	idx, ok := d.slotIndexByID[agentID]
	if !ok || idx < 0 || idx >= len(d.slots) {
		return nil
	}
	return &d.slots[idx]
}

func (d *toolSubagentDisplay) ensureSlot(agentID string, label string) *toolSubagentSlot {
	if idx, ok := d.slotIndexByID[agentID]; ok {
		slot := &d.slots[idx]
		if slot.label == "" {
			slot.label = label
		}
		return slot
	}
	slot := toolSubagentSlot{
		agentID:   agentID,
		label:     label,
		stateKind: toolSubagentSlotStateStarting,
	}
	d.slots = append(d.slots, slot)
	idx := len(d.slots) - 1
	d.slotIndexByID[agentID] = idx
	return &d.slots[idx]
}

func (m *model) renderToolSubagentTextBlock(text string, width int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	rendered := make([]string, 0, len(lines))
	for i, line := range lines {
		prompt := "  "
		if i == 0 {
			prompt = "• "
		}
		for _, wrapped := range tuicontrols.WrapPromptedText(prompt, width, termformatSanitizeLine(line)) {
			rendered = append(rendered, m.styleToolSubagentLine(wrapped))
		}
	}
	return strings.Join(rendered, "\n")
}

func (m *model) indentToolSubagentBlock(block string, prefix string) string {
	if block == "" {
		return ""
	}
	lines := strings.Split(block, "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}

func (m *model) styleToolSubagentLine(s string) string {
	return termformat.Style{Foreground: m.palette.primaryForeground}.Wrap(termformatSanitizeLine(s))
}

func termformatSanitizeLine(s string) string {
	return termformat.Sanitize(strings.ReplaceAll(s, "\n", " "), 4)
}
