package tui

import (
	"fmt"
	"strings"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/agentformatter"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/q/termformat"
	"github.com/codalotl/codalotl/internal/q/tui/tuicontrols"
	"github.com/codalotl/codalotl/internal/tools/spectools"
)

type checkSpecConformanceDisplay struct {
	messageIndex  int
	slots         []checkSpecConformanceSlot
	slotIndexByID map[string]int
}

type checkSpecConformanceSlot struct {
	agentID    string
	label      string
	done       bool
	stateKind  checkSpecConformanceSlotStateKind
	stateEvent agent.Event
	stateText  string
}

type checkSpecConformanceSlotStateKind int

const (
	checkSpecConformanceSlotStateStarting checkSpecConformanceSlotStateKind = iota
	checkSpecConformanceSlotStateLiveEvent
	checkSpecConformanceSlotStateTerminalEvent
	checkSpecConformanceSlotStateTerminalText
)

func (m *model) startCheckSpecConformanceDisplay(ev agent.Event) {
	if ev.Type != agent.EventTypeToolCall || ev.ToolCall == nil {
		return
	}
	if toolName(ev) != spectools.ToolNameCheckSpecConformance {
		return
	}
	callID := ev.ToolCall.CallID
	if callID == "" {
		return
	}
	if _, ok := m.checkSpecDisplays[callID]; ok {
		return
	}
	m.checkSpecDisplays[callID] = &checkSpecConformanceDisplay{
		messageIndex:  len(m.messages),
		slotIndexByID: make(map[string]int),
	}
}

func (m *model) handleCheckSpecConformanceDescendantEvent(ev agent.Event, autoScroll bool) bool {
	scopeRef, ok := m.enclosingNamedToolDisplayScope(ev.Agent, spectools.ToolNameCheckSpecConformance)
	if !ok {
		return false
	}
	scope := m.toolDisplayScope(scopeRef)
	if scope == nil {
		return false
	}
	display := m.checkSpecDisplays[scope.call.CallID]
	if display == nil {
		return false
	}
	directAgentID, ok := m.toolScopeDirectChildAgentID(scopeRef.agentID, ev.Agent.ID)
	if !ok {
		return false
	}

	slot := display.ensureSlot(directAgentID, m.subagentLabels[directAgentID])
	updated := false
	switch ev.Type {
	case agent.EventTypeStartSubagent:
		if directAgentID == ev.Agent.ID {
			if slot.label == "" {
				slot.label = ev.StartSubagent.Label
			}
			updated = true
		}
	case agent.EventTypeAssistantTurnComplete, agent.EventTypeDoneSuccess:
		if directAgentID == ev.Agent.ID && ev.Type == agent.EventTypeDoneSuccess && !slot.done {
			slot.stateKind = checkSpecConformanceSlotStateTerminalText
			slot.stateText = "No final result"
			slot.done = true
			updated = true
		}
	case agent.EventTypeAssistantText:
		if directAgentID == ev.Agent.ID && ev.AssistantTextFinalizing {
			slot.stateKind = checkSpecConformanceSlotStateTerminalText
			slot.stateText = m.formatCheckSpecConformanceFinalText(ev.TextContent.Content)
			slot.done = true
			updated = true
			break
		}
		if !slot.done {
			slot.stateKind = checkSpecConformanceSlotStateLiveEvent
			slot.stateEvent = m.normalizeCheckSpecConformanceSlotEvent(ev)
			updated = true
		}
	case agent.EventTypeError, agent.EventTypeCanceled:
		if directAgentID == ev.Agent.ID && !slot.done {
			slot.stateKind = checkSpecConformanceSlotStateTerminalEvent
			slot.stateEvent = m.normalizeCheckSpecConformanceSlotEvent(ev)
			slot.done = true
			updated = true
			break
		}
		if !slot.done {
			slot.stateKind = checkSpecConformanceSlotStateLiveEvent
			slot.stateEvent = m.normalizeCheckSpecConformanceSlotEvent(ev)
			updated = true
		}
	default:
		if !slot.done {
			slot.stateKind = checkSpecConformanceSlotStateLiveEvent
			slot.stateEvent = m.normalizeCheckSpecConformanceSlotEvent(ev)
			updated = true
		}
	}
	if updated {
		m.invalidateMessage(display.messageIndex)
		m.refreshViewport(autoScroll)
	}
	return true
}

func (m *model) handleCheckSpecConformanceCompletion(ev agent.Event, autoScroll bool) bool {
	if ev.Type != agent.EventTypeToolComplete {
		return false
	}
	if toolName(ev) != spectools.ToolNameCheckSpecConformance {
		return false
	}
	m.appendAgentEvent(ev)
	m.refreshViewport(autoScroll)
	return true
}

func (m *model) renderToolSpecificMessage(msg *chatMessage, width int) (string, bool) {
	if msg == nil || msg.kind != messageKindAgent {
		return "", false
	}
	switch msg.event.Type {
	case agent.EventTypeToolCall:
		if toolName(msg.event) != spectools.ToolNameCheckSpecConformance {
			return "", false
		}
		return m.renderCheckSpecConformanceCallMessage(msg, width), true
	case agent.EventTypeToolComplete:
		if toolName(msg.event) != spectools.ToolNameCheckSpecConformance {
			return "", false
		}
		if msg.event.ToolResult == nil || msg.event.ToolResult.IsError {
			return "", false
		}
		return m.renderCheckSpecConformanceCompleteMessage(msg.event, width), true
	default:
		return "", false
	}
}

func (m *model) renderCheckSpecConformanceCallMessage(msg *chatMessage, width int) string {
	if msg == nil {
		return ""
	}
	header := m.agentFormatter.FormatEvent(msg.event, width)
	display := msg.checkSpecDisplay
	if display == nil {
		display = m.checkSpecDisplays[msg.toolCallID]
	}
	if display == nil || len(display.slots) == 0 {
		return header
	}
	body := m.renderCheckSpecConformanceSlots(display, width)
	if body == "" {
		return header
	}
	return header + "\n" + body
}

func (m *model) renderCheckSpecConformanceCompleteMessage(ev agent.Event, width int) string {
	header := firstLine(m.agentFormatter.FormatEvent(ev, width))
	body := m.renderCheckSpecConformanceSummary(ev.ToolResult)
	if body == "" {
		return header
	}
	return header + "\n" + body
}

func (m *model) renderCheckSpecConformanceSlots(display *checkSpecConformanceDisplay, width int) string {
	if display == nil || len(display.slots) == 0 {
		return ""
	}
	sections := make([]string, 0, len(display.slots))
	for i := range display.slots {
		sections = append(sections, m.renderCheckSpecConformanceSlot(display.slots[i], i, width))
	}
	return strings.Join(sections, "\n")
}

func (m *model) renderCheckSpecConformanceSlot(slot checkSpecConformanceSlot, index int, width int) string {
	label := strings.TrimSpace(slot.label)
	if label == "" {
		label = fmt.Sprintf("%d", index+1)
	}
	header := m.styleCheckSpecConformanceLine("  • Package " + termformatSanitizeLine(label))
	body := m.renderCheckSpecConformanceSlotBody(slot, width)
	if body == "" {
		return header
	}
	return header + "\n" + body
}

func (m *model) renderCheckSpecConformanceSlotBody(slot checkSpecConformanceSlot, width int) string {
	innerWidth := max(width-4, 1)
	switch slot.stateKind {
	case checkSpecConformanceSlotStateTerminalText:
		return m.indentCheckSpecConformanceBlock(m.renderCheckSpecConformanceTextBlock(slot.stateText, innerWidth), "    ")
	case checkSpecConformanceSlotStateLiveEvent, checkSpecConformanceSlotStateTerminalEvent:
		return m.indentCheckSpecConformanceBlock(m.agentFormatter.FormatEvent(slot.stateEvent, innerWidth), "    ")
	default:
		return m.indentCheckSpecConformanceBlock(m.renderCheckSpecConformanceTextBlock("Starting", innerWidth), "    ")
	}
}

func (m *model) renderCheckSpecConformanceSummary(result *llmstream.ToolResult) string {
	if result == nil {
		return m.styleCheckSpecConformanceLine("  └ Invalid SPEC conformance result")
	}
	results, err := spectools.ParseCheckSpecConformanceResults(result.Result)
	if err != nil {
		return m.styleCheckSpecConformanceLine("  └ Invalid SPEC conformance result")
	}
	summary := spectools.SummarizeCheckSpecConformanceResults(results)
	lines := []string{m.styleCheckSpecConformanceLine("  └ " + m.checkSpecConformanceSummaryLine(summary))}
	for _, postcheckErr := range summary.PostcheckErrors {
		lines = append(lines, m.styleCheckSpecConformanceErrorLine(
			fmt.Sprintf("    Postcheck error for %s: %s", termformatSanitizeLine(postcheckErr.Package), termformatSanitizeLine(postcheckErr.Error)),
		))
	}
	return strings.Join(lines, "\n")
}

func (m *model) checkSpecConformanceSummaryLine(summary spectools.CheckSpecConformanceSummary) string {
	var parts []string
	if summary.ConformingCount > 0 {
		parts = append(parts, fmt.Sprintf("%d conforming", summary.ConformingCount))
	}
	if summary.NonconformingCount > 0 {
		parts = append(parts, fmt.Sprintf("%d non-conforming", summary.NonconformingCount))
	}
	if summary.ErrorCount > 0 {
		parts = append(parts, pluralize(summary.ErrorCount, "error", "errors"))
	}
	if len(parts) == 0 {
		return "No eligible packages."
	}
	return strings.Join(parts, ", ")
}

func pluralize(count int, singular string, plural string) string {
	if count == 1 {
		return fmt.Sprintf("%d %s", count, singular)
	}
	return fmt.Sprintf("%d %s", count, plural)
}

func (m *model) formatCheckSpecConformanceFinalText(finalMessage string) string {
	block := spectools.FormatCheckSpecConformancePackageFinalMessage(finalMessage)
	text := strings.TrimSpace(agentformatter.RenderPlainTextBlock(block))
	if text == "" {
		return "Invalid conformance result"
	}
	return text
}

func (m *model) normalizeCheckSpecConformanceSlotEvent(ev agent.Event) agent.Event {
	ev.Agent.Depth = 0
	return ev
}

func (m *model) enclosingNamedToolDisplayScope(meta agent.AgentMeta, tool string) (toolDisplayScopeRef, bool) {
	for agentID := meta.Parent; agentID != ""; agentID = m.agentParents[agentID] {
		scopes := m.activeToolScopes[agentID]
		for i := len(scopes) - 1; i >= 0; i-- {
			if scopes[i].call.Name != tool {
				continue
			}
			return toolDisplayScopeRef{agentID: agentID, index: i}, true
		}
	}
	return toolDisplayScopeRef{}, false
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

func (d *checkSpecConformanceDisplay) ensureSlot(agentID string, label string) *checkSpecConformanceSlot {
	if idx, ok := d.slotIndexByID[agentID]; ok {
		slot := &d.slots[idx]
		if slot.label == "" {
			slot.label = label
		}
		return slot
	}
	slot := checkSpecConformanceSlot{
		agentID:   agentID,
		label:     label,
		stateKind: checkSpecConformanceSlotStateStarting,
	}
	d.slots = append(d.slots, slot)
	idx := len(d.slots) - 1
	d.slotIndexByID[agentID] = idx
	return &d.slots[idx]
}

func (m *model) renderCheckSpecConformanceTextBlock(text string, width int) string {
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
			rendered = append(rendered, m.styleCheckSpecConformanceLine(wrapped))
		}
	}
	return strings.Join(rendered, "\n")
}

func (m *model) indentCheckSpecConformanceBlock(block string, prefix string) string {
	if block == "" {
		return ""
	}
	lines := strings.Split(block, "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}

func (m *model) styleCheckSpecConformanceLine(s string) string {
	return termformat.Style{Foreground: m.palette.primaryForeground}.Wrap(termformatSanitizeLine(s))
}

func (m *model) styleCheckSpecConformanceErrorLine(s string) string {
	return termformat.Style{Foreground: m.palette.redForeground}.Wrap(termformatSanitizeLine(s))
}

func termformatSanitizeLine(s string) string {
	return termformat.Sanitize(strings.ReplaceAll(s, "\n", " "), 4)
}

func firstLine(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		return s[:idx]
	}
	return s
}
