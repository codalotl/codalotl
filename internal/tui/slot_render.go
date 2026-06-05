package tui

import (
	"strings"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/agentformatter"
	"github.com/codalotl/codalotl/internal/q/termformat"
	"github.com/codalotl/codalotl/internal/q/tui/tuicontrols"
)

// toolSubagentDisplay tracks stable subagent slots owned by a tool-call message.
type toolSubagentDisplay struct {
	messageIndex  int                // Message index is the index of the owning tool-call message in model.messages.
	slots         []toolSubagentSlot // Slots are the stable subagent slots in display order.
	slotIndexByID map[string]int     // Slot index by ID maps direct child subagent IDs to indices in slots.
}

// toolSubagentSlot stores display state for one labeled direct subagent slot.
type toolSubagentSlot struct {
	agentID    string                    // Agent ID is the direct child subagent represented by this slot.
	label      string                    // Label is the user-visible stable slot label.
	done       bool                      // Done reports whether the slot has reached a terminal display state.
	stateKind  toolSubagentSlotStateKind // State kind selects how the slot body is rendered.
	stateEvent agent.Event               // State event is the live or terminal event rendered for event states.
	stateText  string                    // State text is the terminal text rendered for text states.
}

// toolSubagentSlotStateKind classifies the content currently displayed in a stable subagent slot.
type toolSubagentSlotStateKind int

const (
	toolSubagentSlotStateStarting toolSubagentSlotStateKind = iota
	toolSubagentSlotStateLiveEvent
	toolSubagentSlotStateTerminalEvent
	toolSubagentSlotStateTerminalText
)

// startToolSubagentDisplay initializes stable-slot display state for a tool-call event.
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

// handleToolSubagentDescendantEvent routes ev into a stable subagent slot owned by an active tool call. Labeled direct subagent start events create slots; later
// events from that subagent or any descendant update the slot, including final presenter text and terminal states. It returns true when the event belongs to such
// a display and should not be rendered by the normal message path. When the slot state changes, it invalidates the owning message and refreshes the viewport, using
// autoScroll to decide whether to scroll to the bottom.
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

// renderToolSpecificMessage renders a detailed tool call or result when it has visible output or stable subagent slots. It returns false when the message should
// use the normal agent formatter.
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

// renderToolMessageWithDetails renders a tool message together with its output and stable subagent slots.
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

// renderToolOutputs renders display-only tool output events as one message block.
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

// renderToolSubagentSlots renders all stable subagent slots for display.
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

// renderToolSubagentSlot renders one stable subagent slot.
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

// renderToolSubagentSlotBody renders the indented body for one stable subagent slot.
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

// formatToolSubagentFinalText formats the final text shown for a subagent slot.
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

// normalizeToolSubagentSlotEvent returns ev adjusted for rendering inside a stable subagent slot.
func (m *model) normalizeToolSubagentSlotEvent(ev agent.Event) agent.Event {
	ev.Agent.Depth = 0
	return ev
}

// owningToolDisplayScope returns the tool display scope that owns rendering for meta.
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

// toolScopeDirectChildAgentID returns the direct child of scopeAgentID that contains agentID.
func (m *model) toolScopeDirectChildAgentID(scopeAgentID string, agentID string) (string, bool) {
	for current := agentID; current != ""; current = m.agentParents[current] {
		if m.agentParents[current] == scopeAgentID {
			return current, true
		}
	}
	return "", false
}

// invalidateMessage clears cached formatted output for the message at index.
func (m *model) invalidateMessage(index int) {
	if index < 0 || index >= len(m.messages) {
		return
	}
	m.messages[index].formatted = ""
	m.messages[index].formattedWidth = 0
}

// slot returns the existing slot for agentID, or nil if no valid slot exists.
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

// ensureSlot returns the slot for agentID, creating a starting slot when needed.
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

// renderToolSubagentTextBlock renders subagent text as a wrapped, styled slot body.
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

// indentToolSubagentBlock prefixes each line of block and returns the indented text.
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

// styleToolSubagentLine sanitizes s and applies the normal subagent line style.
func (m *model) styleToolSubagentLine(s string) string {
	return termformat.Style{Foreground: m.palette.primaryForeground}.Wrap(termformatSanitizeLine(s))
}

func termformatSanitizeLine(s string) string {
	return termformat.Sanitize(strings.ReplaceAll(s, "\n", " "), 4)
}
