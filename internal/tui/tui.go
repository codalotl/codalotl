package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/agentformatter"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/q/termformat"
	qtui "github.com/codalotl/codalotl/internal/q/tui"
	"github.com/codalotl/codalotl/internal/q/tui/tuicontrols"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
)

const (
	minInputLines = 3
	maxInputLines = 10

	historyIndexNone = -1

	// mouseWheelScrollLines is the number of lines to scroll per wheel "click".
	mouseWheelScrollLines = 3
)

type messageKind int

const (
	messageKindSystem messageKind = iota
	messageKindWelcome
	messageKindUser
	messageKindQueuedUser
	messageKindAgent
	messageKindContextStatus
)

type packageContextStatus int

const (
	packageContextStatusPending packageContextStatus = iota
	packageContextStatusSuccess
	packageContextStatusFailure
)

type contextStatusLine struct {
	text   string
	status packageContextStatus
}

type packageContextState struct {
	runID        int
	messageIndex int
	status       packageContextStatus
	packagePath  string
}

type chatMessage struct {
	kind          messageKind
	userMessage   string // the unstyled, unformatted message exactly as the user typed it (also unstyled system messages).
	event         agent.Event
	toolCallID    string
	contextStatus *contextStatusLine

	// The ANSI formatted string. Each formatted must have all styles attached to it.
	// It must be the correct block width (all lines padded with spaces to equal width of the viewport.
	// Background colors must be set on this (if we're not in the uncolored palette).
	// Resize events need to recalculate this.
	formatted      string
	formattedWidth int
}

type agentRun struct {
	cancel context.CancelFunc
	events <-chan agent.Event
	id     int
}

type permissionPrompt struct {
	request authdomain.UserRequest
}

type agentEventMsg struct {
	event agent.Event
	runID int
}

type agentStreamClosedMsg struct {
	runID int
}

type workingIndicatorTickMsg struct{}

type userRequestMsg struct {
	request  authdomain.UserRequest
	sourceID int
}

type packageContextResultMsg struct {
	runID  int
	status packageContextStatus
	text   string
}

// Run launches the TUI in an alternate screen buffer.
func Run() error {
	return RunWithConfig(Config{})
}

// RunWithConfig launches the TUI using the provided configuration.
func RunWithConfig(cfg Config) error {
	// TODO: allow callers to select palette; for now force dark mode for experimentation.
	cfg.Palette = PaletteDark
	palette := newColorPalette(cfg)
	formatterCfg := agentformatter.Config{
		PlainText:       !palette.colorized,
		BackgroundColor: palette.primaryBackground,
		ForegroundColor: palette.primaryForeground,
		AccentColor:     palette.accentForeground,
		ColorfulColor:   palette.colorfulForeground,
		SuccessColor:    palette.greenForeground,
		ErrorColor:      palette.redForeground,
	}
	agentFormatter := agentformatter.NewTUIFormatter(formatterCfg)
	initialCfg := sessionConfig{}
	initialSession, err := newSession(initialCfg)
	if err != nil {
		return err
	}

	model := newModel(palette, agentFormatter, initialSession, initialCfg, newSession)

	return qtui.RunTUI(model, qtui.Options{
		Input:       os.Stdin,
		Output:      os.Stdout,
		Framerate:   60,
		EnableMouse: true, // required for wheel scrolling / MouseEvent delivery
	})
}

type model struct {
	// ready is set on the first window size event. Only render TUI when ready=true
	ready bool

	//
	// At any given time, the window height and window width are windowHeight/windowWidth. From this, we must calculate the height/width of all "controls"/"areas", so that they can be cell-perfect aligned.
	//

	windowHeight int
	windowWidth  int

	// viewportWidth and infoPanelWidth must sum to windowWidth.
	// Below the viewport is the text area. It must be the same width as the viewport.
	viewportWidth  int
	infoPanelWidth int

	viewportHeight       int
	textAreaHeight       int // height of text area AND any border around it
	infoLineHeight       int // height of info area below the text area (ex: could show things like common hotkeys, context usage, etc)
	permissionViewHeight int // 0 if not shown (activePermission == nil)

	// Implied:
	//   - text area width == viewportWidth
	//   - permission view width == viewportWidth
	//   - info panel height == windowHeight

	messages []chatMessage

	messageHistory      []string
	editedHistoryDrafts map[int]string
	cyclingMode         bool
	cycleIndex          int
	editingHistoryIndex int

	viewport *tuicontrols.View
	textarea *tuicontrols.TextArea
	tui      *qtui.TUI

	session        *session
	sessionConfig  sessionConfig
	sessionFactory func(sessionConfig) (*session, error)

	agentFormatter agentformatter.Formatter

	messageQueue   []string
	currentRun     *agentRun
	runStartedAt   time.Time
	nextAgentRunID int

	workingIndicatorAnimationPos int
	workingIndicatorTickerCancel qtui.CancelFunc

	// When `/new` is invoked mid-run we mark the reset as pending so the cleanup
	// happens only after `agentStreamClosedMsg` fires; that way we don't tear
	// down session state while events are still draining from the agent.
	pendingSessionConfig *sessionConfig

	permissionQueue    []*permissionPrompt
	activePermission   *permissionPrompt
	permissionViewText string

	requests      <-chan authdomain.UserRequest
	requestSource int
	requestCancel context.CancelFunc

	palette colorPalette

	packageContext       *packageContextState
	nextPackageContextID int
}

func newModel(palette colorPalette, formatter agentformatter.Formatter, initialSession *session, initialCfg sessionConfig, factory func(sessionConfig) (*session, error)) *model {
	ti := newTextArea()
	vp := tuicontrols.NewView(0, 0)
	vp.SetEmptyLineBackgroundColor(palette.primaryBackground)

	activeCfg := initialCfg
	if initialSession != nil {
		activeCfg = initialSession.config
	}

	m := &model{
		viewport:            vp,
		textarea:            ti,
		session:             initialSession,
		sessionFactory:      factory,
		sessionConfig:       activeCfg,
		agentFormatter:      formatter,
		requestSource:       1,
		nextAgentRunID:      1,
		messages:            make([]chatMessage, 0, 32),
		messageHistory:      make([]string, 0, 32),
		messageQueue:        make([]string, 0),
		permissionQueue:     make([]*permissionPrompt, 0),
		palette:             palette,
		cycleIndex:          historyIndexNone,
		editingHistoryIndex: historyIndexNone,
	}
	if initialSession != nil {
		m.requests = initialSession.UserRequests()
		m.messages = append(m.messages, chatMessage{kind: messageKindWelcome})
	}
	m.updatePlaceholder()
	return m
}

func (m *model) Init(t *qtui.TUI) {
	m.tui = t
	if m.requests != nil {
		m.startUserRequestListener(m.requestSource, m.requests)
	}
}

func (m *model) Update(t *qtui.TUI, msg qtui.Message) {
	if m.tui == nil && t != nil {
		m.tui = t
	}

	// Only send certain key events to viewport.
	sendMsgToViewport := false
	switch ev := msg.(type) {
	case qtui.KeyEvent:
		switch ev.ControlKey {
		case qtui.ControlKeyPageUp, qtui.ControlKeyPageDown: // NOTE: shift+up/down don't work (usually not mapped; just sends Up/Down)
			sendMsgToViewport = true
		}
		if sendMsgToViewport && m.viewport != nil {
			m.viewport.Update(t, ev)
		}
		skipTextarea := m.handleKeyEvent(ev)
		if !skipTextarea && !sendMsgToViewport && m.textarea != nil {
			m.textarea.Update(t, ev)
		}
	case qtui.MouseEvent:
		m.handleMouseEvent(ev)
	case qtui.ResizeEvent:
		m.handleWindowSize(ev)
	case qtui.SigTermEvent:
		m.stopAgentRun()
		m.stopUserRequestListener()
		if m.session != nil {
			m.session.Close()
		}
	case qtui.SigIntEvent:
		m.stopAgentRun()
		m.stopUserRequestListener()
		if m.session != nil {
			m.session.Close()
		}
	case agentEventMsg:
		if m.currentRun != nil && ev.runID == m.currentRun.id {
			m.handleAgentEvent(ev.event)
		}
	case agentStreamClosedMsg:
		if m.currentRun != nil && ev.runID == m.currentRun.id {
			pendingCfg := m.pendingSessionConfig
			m.pendingSessionConfig = nil
			m.finishAgentRun()
			if pendingCfg != nil {
				m.resetSessionWithConfig(*pendingCfg)
			} else {
				m.startNextQueuedMessage()
			}
		}
	case workingIndicatorTickMsg:
		if m.isAgentRunning() {
			m.refreshViewport(false)
		}
	case userRequestMsg:
		if ev.sourceID == m.requestSource {
			m.enqueuePermissionRequest(ev.request)
		}
	case packageContextResultMsg:
		m.handlePackageContextResult(ev)
	}

	m.persistEditedHistoryDraft()
	m.updateTextareaHeight()
}

func (m *model) View() string {
	if !m.ready {
		return "initializing"
	}
	const minHeightToRender = 15
	const minWidthToRender = 30
	if m.windowHeight < minHeightToRender || m.windowWidth < minWidthToRender {
		return "window too small\nmake it bigger"
	}

	var b strings.Builder

	viewportBlock := ""
	if m.viewport != nil {
		viewportBlock = m.viewport.View()
	}

	textAreaBlock := ""
	if m.textarea != nil {
		m.textarea.BackgroundColor = m.palette.accentBackground
		m.textarea.ForegroundColor = m.palette.primaryForeground
		m.textarea.PlaceholderColor = m.palette.accentForeground
		m.textarea.CaretColor = m.palette.primaryForeground
		textAreaBlock = m.textarea.View()
		textAreaBlock = termformat.BlockStyle{MarginTop: 1, MarginLeft: 1, MarginRight: 1, MarginBackground: m.palette.primaryBackground}.Apply(textAreaBlock)
	}

	b.WriteString(viewportBlock)
	b.WriteString("\n")
	if perm := m.permissionView(); perm != "" {
		b.WriteString(perm)
		b.WriteString("\n")
	}
	b.WriteString(textAreaBlock)
	if m.infoLineHeight > 0 {
		b.WriteString("\n")
		b.WriteString(m.infoLineView())
	}

	if m.infoPanelWidth == 0 {
		return b.String()
	}

	infoBlock := m.infoPanelBlock()

	blocks := []termformat.LayoutBlock{
		{Block: b.String(), X: 0, Y: 0},
		{Block: infoBlock, X: m.viewportWidth, Y: 0},
	}
	combo, err := termformat.Layout(blocks, nil)
	if err == nil {
		debugLogf("h=%d lines=%d rectHeight=%d", m.windowHeight, len(strings.Split(combo, "\n")), termformat.BlockHeight(combo))
		return combo
	} else {
		return fmt.Sprintf("rendering error: %v", err)
	}
}

func (m *model) handleWindowSize(msg qtui.ResizeEvent) {
	debugLogf("resize event: w=%v h=%v\n", msg.Width, msg.Height)
	m.windowHeight = msg.Height
	m.windowWidth = msg.Width
	m.updateSizes()
	if m.viewport != nil {
		m.viewport.ScrollToBottom()
	}
	m.refreshPermissionView()
	m.refreshViewport(true)
	m.ready = true
}

func (m *model) handleMouseEvent(ev qtui.MouseEvent) {
	if m.viewport == nil {
		return
	}
	if !ev.IsWheel() {
		return
	}

	switch ev.Button {
	case qtui.MouseButtonWheelUp:
		m.viewport.ScrollUp(mouseWheelScrollLines)
	case qtui.MouseButtonWheelDown:
		m.viewport.ScrollDown(mouseWheelScrollLines)
	}
}

// updateSizes calculates all sizes (dimensions) on m based on m.windowHeight and m.windowWidth.
// It updates fields on m (ex: m.viewportWidth), and also dimensions on any "components" we're using.
// This method is cheap to call, and can be called idempotently.
func (m *model) updateSizes() {
	m.viewportWidth, m.infoPanelWidth = viewportInfoPanelWidths(m.windowWidth)

	// textAreaHeight is set elsehwere. It's basically going to be a value between 4 and 11.
	if m.textAreaHeight <= 0 {
		m.textAreaHeight = 4
	}

	// permissionViewHeight is set elsewhere. It's going to be 0 if the permission check view isn't shown, or around 5-10 when it is.
	if m.activePermission == nil {
		m.permissionViewHeight = 0
	}

	m.infoLineHeight = 1

	m.viewportHeight = max(m.windowHeight-m.textAreaHeight-m.permissionViewHeight-m.infoLineHeight, 0)

	// debugLogf("sizes: w=%d h=%d vph=%d permH=%d taH=%d\n", m.windowHeight, m.windowHeight, m.viewportHeight, m.permissionViewHeight, m.textAreaHeight)

	if m.viewport != nil {
		m.viewport.SetSize(m.viewportWidth, m.viewportHeight)
		m.viewport.SetEmptyLineBackgroundColor(m.palette.primaryBackground)
	}
	if m.textarea != nil {
		m.textarea.SetSize(m.viewportWidth-2, m.textAreaHeight-1) // 2: margin left/right; 1: margin top
	}
}

// viewportInfoPanelWidths returns the viewport width (messages area - left) and info panel width (right area).
// If the terminal is too small, don't show the info panel area (width=0).
func viewportInfoPanelWidths(terminalWidth int) (int, int) {
	minViewport := 60
	minInfoPanel := 40
	maxInfoPanel := 80
	minCombo := minViewport + minInfoPanel

	if terminalWidth < minCombo {
		return terminalWidth, 0
	}

	info := clamp((2*terminalWidth)/5, minInfoPanel, maxInfoPanel)
	viewport := terminalWidth - info

	return viewport, info
}

func (m *model) handleKeyEvent(key qtui.KeyEvent) (skipTextarea bool) {
	if key.ControlKey == qtui.ControlKeyCtrlC {
		// Ctrl-C is "stop agent" when the agent is currently running; otherwise it quits
		// the app (keeping the bottom help text intact as-is).
		if m.isAgentRunning() {
			m.stopAgentRun()
			if len(m.messageQueue) > 0 {
				m.restoreQueuedMessagesToInput()
			}
			return true
		}

		m.stopAgentRun()
		m.stopUserRequestListener()
		if m.session != nil {
			m.session.Close()
		}
		if m.tui != nil {
			m.tui.Quit()
		}
		return true
	}

	if m.activePermission != nil {
		return m.handlePermissionKey(key)
	}

	if m.cyclingMode && m.shouldExitCyclingForKey(key) {
		m.exitCyclingModeForEditing()
	}

	switch key.ControlKey {
	case qtui.ControlKeyEsc:
		if m.cyclingMode {
			m.exitCyclingModeToDefault()
			return true
		}
		if m.isEditingHistory() {
			m.reenterCyclingModeFromEditing()
			return true
		}
		if m.isAgentRunning() {
			m.stopAgentRun()
			if len(m.messageQueue) > 0 {
				m.restoreQueuedMessagesToInput()
			}
		}
		return true
	case qtui.ControlKeyEnter:
		value := ""
		if m.textarea != nil {
			value = m.textarea.Contents()
		}
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			if m.textarea != nil {
				m.textarea.SetContents("")
			}
			m.exitEditingState()
			return true
		}
		if strings.HasPrefix(trimmed, "/") {
			m.recordSubmittedMessage(value)
			handled := m.handleSlashCommand(trimmed)
			if m.textarea != nil {
				m.textarea.SetContents("")
			}
			if handled {
				return true
			}
			return true
		}
		m.recordSubmittedMessage(value)
		if m.textarea != nil {
			m.textarea.SetContents("")
		}
		m.sendOrQueueMessage(value)
		m.startAgentRunIfPossible(value)
		return true
	case qtui.ControlKeyUp:
		if m.cyclingMode {
			m.cyclePrevious()
			return true
		}
		if m.textarea != nil && m.textarea.Contents() == "" && m.enterCyclingMode() {
			return true
		}
		return false
	case qtui.ControlKeyDown:
		if m.cyclingMode {
			m.cycleNext()
			return true
		}
		return false
	}

	return false
}

func (m *model) handleSlashCommand(cmd string) bool {
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return true
	}

	switch fields[0] {
	case "/quit", "/exit", "/logout":
		m.stopAgentRun()
		m.stopUserRequestListener()
		m.pendingSessionConfig = nil
		m.appendSystemMessage("Ending session.")
		m.refreshViewport(true)
		if m.viewport != nil {
			m.viewport.ScrollToBottom()
		}
		if m.session != nil {
			m.session.Close()
		}
		if m.tui != nil {
			m.tui.Quit()
		}
		return true
	case "/new":
		m.handleNewSessionCommand()
		return true
	case "/package":
		packageArg := strings.TrimSpace(strings.TrimPrefix(cmd, "/package"))
		m.handlePackageCommand(packageArg)
		return true
	case "/generic":
		m.handleGenericCommand()
		return true
	case "/fake":
		if m.isAgentRunning() {
			m.appendSystemMessage("Finish the current run before starting /fake.")
			m.refreshViewport(true)
			if m.viewport != nil {
				m.viewport.ScrollToBottom()
			}
			return true
		}
		m.appendSystemMessage("Simulating agent activity...")
		m.refreshViewport(true)
		if m.viewport != nil {
			m.viewport.ScrollToBottom()
		}
		m.startFakeAgentRun()
		return true
	case "/permission":
		m.triggerPermissionDemo()
		return true
	default:
		m.appendSystemMessage(fmt.Sprintf("Command %s not supported.", cmd))
		m.refreshViewport(true)
		return true
	}
}

func (m *model) handleNewSessionCommand() {
	cfg := m.sessionConfig
	message := ""
	if m.isAgentRunning() {
		message = "Stopping current task before starting a new session..."
	}
	m.requestSessionReset(cfg, message)
}

func (m *model) handleGenericCommand() {
	m.requestSessionReset(sessionConfig{}, "Package mode disabled. Use `/package path/to/pkg` (path relative to sandbox) to select a package.")
}

func (m *model) handlePackageCommand(arg string) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		m.handleGenericCommand()
		return
	}

	cfg, err := m.normalizeConfigForCurrentSandbox(sessionConfig{packagePath: arg})
	if err != nil {
		m.appendSystemMessage(fmt.Sprintf("Cannot enter package mode: %v", err))
		m.refreshViewport(true)
		if m.viewport != nil {
			m.viewport.ScrollToBottom()
		}
		return
	}

	m.requestSessionReset(cfg, "Switching to package mode...")
}

func (m *model) requestSessionReset(cfg sessionConfig, message string) {
	message = strings.TrimSpace(message)
	if m.isAgentRunning() {
		if m.pendingSessionConfig != nil {
			return
		}
		pending := cfg
		m.pendingSessionConfig = &pending
		m.messageQueue = nil
		m.rejectOutstandingPermissions()
		if message != "" {
			m.appendSystemMessage(message)
			m.refreshViewport(true)
			if m.viewport != nil {
				m.viewport.ScrollToBottom()
			}
		}
		m.stopAgentRun()
		return
	}

	m.resetSessionWithConfig(cfg)
}

func (m *model) normalizeConfigForCurrentSandbox(cfg sessionConfig) (sessionConfig, error) {
	sandboxDir := ""
	switch {
	case m != nil && m.session != nil && m.session.sandboxDir != "":
		sandboxDir = m.session.sandboxDir
	default:
		var err error
		sandboxDir, err = determineSandboxDir()
		if err != nil {
			return cfg, err
		}
	}

	normalizedCfg, _, err := normalizeSessionConfig(cfg, sandboxDir)
	return normalizedCfg, err
}

func (m *model) shouldExitCyclingForKey(msg qtui.KeyEvent) bool {
	if !m.cyclingMode {
		return false
	}

	switch msg.ControlKey {
	case qtui.ControlKeyUp, qtui.ControlKeyDown, qtui.ControlKeyEsc, qtui.ControlKeyEnter, qtui.ControlKeyPageUp, qtui.ControlKeyPageDown:
		return false
	}

	return true
}

func (m *model) handlePermissionKey(msg qtui.KeyEvent) bool {
	if m.activePermission == nil {
		return true
	}

	switch msg.ControlKey {
	case qtui.ControlKeyEsc:
		m.resolvePermission(false)
		m.stopAgentRun()
		if len(m.messageQueue) > 0 {
			m.restoreQueuedMessagesToInput()
		}
		return true
	case qtui.ControlKeyNone:
		if !msg.IsRunes() {
			return true
		}
		switch strings.ToLower(string(msg.Runes)) {
		case "y":
			m.resolvePermission(true)
			return true
		case "n":
			m.resolvePermission(false)
			return true
		}
	}

	return true
}

func (m *model) resolvePermission(allow bool) {
	if m.activePermission == nil {
		return
	}
	req := m.activePermission.request
	if allow {
		req.Allow()
	} else {
		req.Disallow()
	}
	m.activePermission = nil
	m.refreshPermissionView()
	m.refreshViewport(true)
	m.advancePermissionQueue()
}

func (m *model) recordSubmittedMessage(value string) {
	m.exitEditingState()
	if len(m.editedHistoryDrafts) > 0 {
		m.editedHistoryDrafts = nil
	}
	if m.shouldSaveToHistory(value) {
		m.messageHistory = append(m.messageHistory, value)
	}
}

func (m *model) shouldSaveToHistory(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	if !strings.HasPrefix(trimmed, "/") {
		return true
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return false
	}
	switch fields[0] {
	case "/new", "/model", "/quit", "/exit", "/logout":
		return false
	}
	return len(fields) > 1
}

func (m *model) enterCyclingMode() bool {
	if len(m.messageHistory) == 0 {
		return false
	}
	m.cyclingMode = true
	m.editingHistoryIndex = historyIndexNone
	m.cycleIndex = len(m.messageHistory) - 1
	m.showHistoryEntry(m.cycleIndex)
	return true
}

func (m *model) cyclePrevious() {
	if !m.cyclingMode {
		return
	}
	if m.cycleIndex <= 0 {
		m.cycleIndex = 0
		m.showHistoryEntry(m.cycleIndex)
		return
	}
	m.cycleIndex--
	m.showHistoryEntry(m.cycleIndex)
}

func (m *model) cycleNext() {
	if !m.cyclingMode {
		return
	}
	if m.cycleIndex >= len(m.messageHistory)-1 {
		m.exitCyclingModeToDefault()
		return
	}
	m.cycleIndex++
	m.showHistoryEntry(m.cycleIndex)
}

func (m *model) exitCyclingModeToDefault() {
	m.exitEditingState()
	if m.textarea != nil {
		m.textarea.SetContents("")
	}
	m.updateTextareaHeight()
}

func (m *model) exitCyclingModeForEditing() {
	if !m.cyclingMode {
		return
	}
	m.editingHistoryIndex = m.cycleIndex
	m.cyclingMode = false
	m.cycleIndex = historyIndexNone
}

func (m *model) reenterCyclingModeFromEditing() {
	if !m.isEditingHistory() || len(m.messageHistory) == 0 {
		return
	}
	index := m.editingHistoryIndex
	if index >= len(m.messageHistory) {
		index = len(m.messageHistory) - 1
	}
	m.cyclingMode = true
	m.editingHistoryIndex = historyIndexNone
	m.cycleIndex = index
	m.showHistoryEntry(index)
}

func (m *model) exitEditingState() {
	m.cyclingMode = false
	m.cycleIndex = historyIndexNone
	m.editingHistoryIndex = historyIndexNone
}

func (m *model) isEditingHistory() bool {
	return m.editingHistoryIndex != historyIndexNone
}

func (m *model) showHistoryEntry(index int) {
	if index < 0 || index >= len(m.messageHistory) {
		return
	}
	value := m.historyValue(index)
	if m.textarea != nil {
		m.textarea.SetContents(value)
		m.textarea.MoveToBeginningOfText()
	}
	m.updateTextareaHeight()
}

func (m *model) historyValue(index int) string {
	if index < 0 || index >= len(m.messageHistory) {
		return ""
	}
	if edited, ok := m.editedHistoryDrafts[index]; ok {
		return edited
	}
	return m.messageHistory[index]
}

func (m *model) persistEditedHistoryDraft() {
	if !m.isEditingHistory() {
		return
	}
	if m.editingHistoryIndex < 0 || m.editingHistoryIndex >= len(m.messageHistory) {
		m.editingHistoryIndex = historyIndexNone
		return
	}
	if m.editedHistoryDrafts == nil {
		m.editedHistoryDrafts = make(map[int]string)
	}
	if m.textarea != nil {
		m.editedHistoryDrafts[m.editingHistoryIndex] = m.textarea.Contents()
	}
}

func (m *model) sendOrQueueMessage(value string) {
	if m.isAgentRunning() || m.packageContextPending() {
		m.messageQueue = append(m.messageQueue, value)
		m.appendUserMessage(value, true)
		m.refreshViewport(true)
		if m.viewport != nil {
			m.viewport.ScrollToBottom()
		}
		return
	}

	m.appendUserMessage(value, false)
	m.refreshViewport(true)
	if m.viewport != nil {
		m.viewport.ScrollToBottom()
	}
}

func (m *model) startAgentRunIfPossible(value string) {
	if m.session == nil || m.isAgentRunning() || m.packageContextPending() {
		return
	}
	m.startAgentRun(value)
}

func (m *model) startAgentRun(value string) {
	if m.session == nil || m.tui == nil {
		return
	}

	_ = m.session.AddGrantsFromUserMessage(value)

	ctx, cancel := context.WithCancel(context.Background())
	events := m.session.SendMessage(ctx, value)
	if events == nil {
		cancel()
		return
	}
	runID := m.nextAgentRunID
	m.nextAgentRunID++
	m.currentRun = &agentRun{
		cancel: cancel,
		events: events,
		id:     runID,
	}
	m.runStartedAt = time.Now()
	m.workingIndicatorAnimationPos = 0
	m.startWorkingIndicatorTicker()
	m.updatePlaceholder()
	m.refreshViewport(m.shouldAutoScrollOnUpdate())
	m.forwardAgentEvents(runID, events)
}

func (m *model) startFakeAgentRun() {
	if m.tui == nil || m.isAgentRunning() {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	fakeEvents := fakeAgentEvents()
	ch := make(chan agent.Event)

	runID := m.nextAgentRunID
	m.nextAgentRunID++
	m.currentRun = &agentRun{
		cancel: cancel,
		events: ch,
		id:     runID,
	}
	m.runStartedAt = time.Now()
	m.workingIndicatorAnimationPos = 0
	m.startWorkingIndicatorTicker()
	m.updatePlaceholder()
	m.refreshViewport(m.shouldAutoScrollOnUpdate())
	m.forwardAgentEvents(runID, ch)

	go func() {
		defer close(ch)
		defer cancel()
		for i, ev := range fakeEvents {
			if i > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Millisecond * 100):
				}
			}
			select {
			case <-ctx.Done():
				return
			case ch <- ev:
			}
		}
	}()
}

func (m *model) forwardAgentEvents(runID int, events <-chan agent.Event) {
	if events == nil || m.tui == nil {
		return
	}
	prog := m.tui
	go func(run int, ch <-chan agent.Event) {
		for ev := range ch {
			prog.Send(agentEventMsg{event: ev, runID: run})
		}
		prog.Send(agentStreamClosedMsg{runID: run})
	}(runID, events)
}

func (m *model) stopAgentRun() {
	if m.currentRun != nil {
		m.currentRun.cancel()
	}
}

func (m *model) finishAgentRun() {
	m.stopWorkingIndicatorTicker()
	m.currentRun = nil
	m.runStartedAt = time.Time{}
	m.workingIndicatorAnimationPos = 0
	m.updatePlaceholder()
	m.refreshViewport(m.shouldAutoScrollOnUpdate())
}

func (m *model) startWorkingIndicatorTicker() {
	m.stopWorkingIndicatorTicker()
	if m.tui == nil {
		return
	}
	m.workingIndicatorTickerCancel = m.tui.SendPeriodically(workingIndicatorTickMsg{}, time.Second)
}

func (m *model) stopWorkingIndicatorTicker() {
	if m.workingIndicatorTickerCancel != nil {
		m.workingIndicatorTickerCancel()
		m.workingIndicatorTickerCancel = nil
	}
}

func (m *model) startNextQueuedMessage() {
	if len(m.messageQueue) == 0 || m.session == nil || m.packageContextPending() {
		return
	}

	next := m.messageQueue[0]
	m.messageQueue = m.messageQueue[1:]
	m.appendUserMessage(next, false)
	m.refreshViewport(true)
	if m.viewport != nil {
		m.viewport.ScrollToBottom()
	}
	m.startAgentRun(next)
}

func (m *model) startUserRequestListener(sourceID int, requests <-chan authdomain.UserRequest) {
	m.stopUserRequestListener()
	if requests == nil || m.tui == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.requestCancel = cancel
	prog := m.tui
	go func(id int, ch <-chan authdomain.UserRequest, c context.Context) {
		for {
			select {
			case <-c.Done():
				return
			case req, ok := <-ch:
				if !ok {
					return
				}
				prog.Send(userRequestMsg{request: req, sourceID: id})
			}
		}
	}(sourceID, requests, ctx)
}

func (m *model) stopUserRequestListener() {
	if m.requestCancel != nil {
		m.requestCancel()
		m.requestCancel = nil
	}
}

func (m *model) restoreQueuedMessagesToInput() {
	if len(m.messageQueue) == 0 {
		return
	}
	m.exitEditingState()
	if m.textarea != nil {
		m.textarea.SetContents(strings.Join(m.messageQueue, "\n"))
		m.textarea.MoveToEndOfText()
	}
	m.messageQueue = nil
	m.updateTextareaHeight()
}

func (m *model) appendUserMessage(value string, queued bool) {
	kind := messageKindUser
	if queued {
		kind = messageKindQueuedUser
	}
	m.messages = append(m.messages, chatMessage{
		kind:        kind,
		userMessage: value,
	})
}

func (m *model) appendSystemMessage(value string) {
	m.messages = append(m.messages, chatMessage{
		kind:        messageKindSystem,
		userMessage: value,
	})
}

func (m *model) appendContextStatusMessage(text string, status packageContextStatus) int {
	msg := chatMessage{
		kind:          messageKindContextStatus,
		userMessage:   text,
		contextStatus: &contextStatusLine{text: text, status: status},
	}
	m.messages = append(m.messages, msg)
	return len(m.messages) - 1
}

func (m *model) updateContextStatusMessage(index int, status packageContextStatus) {
	if index < 0 || index >= len(m.messages) {
		return
	}
	msg := &m.messages[index]
	if msg.contextStatus == nil {
		msg.contextStatus = &contextStatusLine{text: msg.userMessage, status: status}
	} else {
		msg.contextStatus.status = status
	}
	msg.formatted = ""
	msg.formattedWidth = 0
}

func (m *model) handleAgentEvent(ev agent.Event) {
	autoScroll := m.shouldAutoScrollOnUpdate()

	switch ev.Type {
	case agent.EventTypeAssistantTurnComplete:
		m.refreshViewport(autoScroll)
		return
	case agent.EventTypeDoneSuccess:
		return
	}

	if ev.Type == agent.EventTypeToolComplete {
		if id := eventToolCallID(ev); id != "" && shouldReplaceToolCallWithResult(ev) && m.replaceToolEvent(id, ev) {
			m.refreshViewport(autoScroll)
			return
		}
	}

	m.appendAgentEvent(ev)
	m.refreshViewport(autoScroll)
}

func (m *model) handlePackageContextResult(msg packageContextResultMsg) {
	if m.packageContext == nil || m.packageContext.runID != msg.runID {
		return
	}

	m.packageContext.status = msg.status
	m.updateContextStatusMessage(m.packageContext.messageIndex, msg.status)

	if msg.text != "" && m.session != nil && m.session.agent != nil {
		if err := m.session.agent.AddUserTurn(msg.text); err != nil {
			m.appendSystemMessage(fmt.Sprintf("Failed to apply package context: %v", err))
			m.updateContextStatusMessage(m.packageContext.messageIndex, packageContextStatusFailure)
		}
	}

	if !m.isAgentRunning() {
		m.startNextQueuedMessage()
	}
	m.refreshViewport(true)
}

func (m *model) shouldAutoScrollOnUpdate() bool {
	// Only auto-scroll if the user was already at the bottom. This makes manual
	// scrolling (mouse wheel / page up) usable during streaming output.
	if m == nil || m.viewport == nil {
		return true
	}
	return m.viewport.AtBottom()
}

// withForegroundColor wraps str with ANSI codes for foreground styling. If !accent, uses the primary foreground color. Otherwise, the background color.
func (m *model) withForegroundColor(str string, accent bool) string {
	var color termformat.Color
	if accent {
		color = m.palette.accentForeground
	} else {
		color = m.palette.primaryForeground
	}
	return termformat.Style{Foreground: color}.Wrap(str)
}

func (m *model) appendAgentEvent(ev agent.Event) {
	msg := chatMessage{
		kind:       messageKindAgent,
		event:      ev,
		toolCallID: eventToolCallID(ev),
	}
	m.messages = append(m.messages, msg)
}

func (m *model) replaceToolEvent(callID string, ev agent.Event) bool {
	if callID == "" {
		return false
	}
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].toolCallID == callID {
			m.messages[i].event = ev
			m.messages[i].formatted = "" // clear formatted cache
			return true
		}
	}
	return false
}

func eventToolCallID(ev agent.Event) string {
	if ev.ToolCall != nil {
		return ev.ToolCall.CallID
	}
	if ev.ToolResult != nil {
		return ev.ToolResult.CallID
	}
	return ""
}

func toolName(ev agent.Event) string {
	if ev.ToolResult != nil && ev.ToolResult.Name != "" {
		return ev.ToolResult.Name
	}
	if ev.ToolCall != nil && ev.ToolCall.Name != "" {
		return ev.ToolCall.Name
	}
	return ev.Tool
}

func shouldReplaceToolCallWithResult(ev agent.Event) bool {
	switch toolName(ev) {
	case "change_api", "update_usage", "clarify_public_api":
		// SubAgent tools: we want to show the call *and* the result as separate messages.
		return false
	default:
		return true
	}
}

// refreshViewport calculates the contents of the viewport, calls SetContent on it, and optionally scrolls to the bottom.
func (m *model) refreshViewport(autoScroll bool) {
	width := agentformatter.MinTerminalWidth
	height := 0
	if m.viewport != nil {
		if w := m.viewport.Width(); w > 0 {
			width = w
		}
		height = m.viewport.Height()
	}

	// Ensure all messaages have .formatted set on it for the given width (which we will essentially just concatenate).
	for i := range m.messages {
		m.ensureMessageFormatted(&m.messages[i], width)
	}

	rendered := make([]string, 0, len(m.messages))
	for i := range m.messages {
		rendered = append(rendered, m.messages[i].formatted)
	}

	rendered = m.applyWorkingIndicator(rendered, width)
	rendered = append(rendered, m.blankRow(width, m.palette.primaryBackground)) // always have one blank line at the end
	content := m.joinRenderedBlocks(rendered, width)
	content = m.padViewportContentHeight(content, height, width)
	if m.viewport != nil {
		m.viewport.SetContent(content)
		if autoScroll {
			m.viewport.ScrollToBottom()
		}
	}
}

func (m *model) ensureMessageFormatted(msg *chatMessage, width int) {
	if width <= 0 {
		width = agentformatter.MinTerminalWidth
	}
	if msg.formatted != "" && msg.formattedWidth == width {
		return
	}
	content := ""

	needBgAndWidth := false

	switch msg.kind {
	case messageKindWelcome:
		content = newSessionBlock(width, m.palette, m.sessionConfig)
	case messageKindSystem:
		content = m.withForegroundColor(termformat.Sanitize(msg.userMessage, 4), true)
		needBgAndWidth = true
	case messageKindContextStatus:
		content = m.renderContextStatusLine(msg.contextStatus)
		needBgAndWidth = true
	case messageKindUser:
		content = m.renderUserMessageBlock(msg.userMessage, false, width)
	case messageKindQueuedUser:
		content = m.renderUserMessageBlock(msg.userMessage, true, width)
	case messageKindAgent:
		content = m.agentFormatter.FormatEvent(msg.event, width)
		needBgAndWidth = true
	default:
		content = termformat.Sanitize(msg.userMessage, 4)
		needBgAndWidth = true
	}

	if needBgAndWidth {
		msg.formatted = m.setMessageWidthBG(content, width, m.palette.primaryBackground)
		msg.formattedWidth = width
	} else {
		msg.formatted = content
		msg.formattedWidth = width
	}

}

// applyWorkingIndicator accepts the rendered list of messages in the viewport, returns a new list with a working indicator if the agent is running.
//
// For now, applyWorkingIndicator always just appends a new message. In the future, it may mutate the last entry (eg, a reasoning entry).
func (m *model) applyWorkingIndicator(rendered []string, width int) []string {
	if !m.isAgentRunning() {
		return rendered
	}
	indicator := m.renderWorkingIndicator(width)
	if indicator == "" {
		return rendered
	}
	return append(rendered, indicator)
}

// renderUserMessageBlock returns a fully formated message with proper width and bg color.
func (m *model) renderUserMessageBlock(content string, queued bool, width int) string {
	lines := strings.Split(termformat.Sanitize(content, 4), "\n")
	for i, line := range lines {
		switch i {
		case 0:
			if queued {
				line = fmt.Sprintf("%s (queued)", line)
			}
			lines[i] = "› " + line
		default:
			lines[i] = "  " + line
		}
	}

	content = termformat.Style{Foreground: m.palette.primaryForeground}.Wrap(strings.Join(lines, "\n"))
	bs := termformat.BlockStyle{TotalWidth: width, TextBackground: m.palette.accentBackground, MarginLeft: 1, MarginRight: 1, MarginBackground: m.palette.primaryBackground}

	return bs.Apply(content)
}

func (m *model) renderWorkingIndicator(width int) string {
	text := m.workingIndicatorText()
	if text == "" {
		return ""
	}
	styled := termformat.Style{
		Foreground: m.palette.accentForeground,
	}.Wrap(termformat.Sanitize(text, 4))
	background := m.palette.primaryBackground
	return m.setMessageWidthBG(styled, width, background)
}

func (m *model) renderContextStatusLine(line *contextStatusLine) string {
	if line == nil {
		return ""
	}
	text := strings.TrimSpace(line.text)
	if text == "" {
		return ""
	}
	text = strings.ReplaceAll(text, "\n", " ")
	text = termformat.Sanitize(text, 4)

	bulletColor := m.palette.accentForeground
	switch line.status {
	case packageContextStatusSuccess:
		bulletColor = m.palette.greenForeground
	case packageContextStatusFailure:
		bulletColor = m.palette.redForeground
	}

	bullet := termformat.Style{Foreground: bulletColor}.Wrap("•")
	rest := termformat.Style{Foreground: m.palette.accentForeground}.Wrap(text)
	return bullet + " " + rest
}

func (m *model) workingIndicatorText() string {
	if !m.isAgentRunning() {
		return ""
	}
	elapsed := time.Duration(0)
	if !m.runStartedAt.IsZero() {
		elapsed = time.Since(m.runStartedAt)
		if elapsed < 0 {
			elapsed = 0
		}
	}
	runtime := formatStopwatchDuration(elapsed)
	if runtime == "" {
		return ""
	}
	return fmt.Sprintf("• Working (%s • ESC to interrupt)", runtime)
}

// setMessageWidthBG accepts content as ANSI styled text. It ensures it's width wide with the specified background.
func (m *model) setMessageWidthBG(content string, width int, background termformat.Color) string {
	style := termformat.BlockStyle{
		TotalWidth:         max(width, 1),
		TextBackground:     background,
		BlockNormalizeMode: termformat.BlockNormalizeModeExtend,
	}

	return style.Apply(content)
}

func (m *model) joinRenderedBlocks(blocks []string, width int) string {
	if len(blocks) == 0 {
		return ""
	}
	separator := m.blankRow(width, m.palette.primaryBackground)

	var b strings.Builder
	for i, block := range blocks {
		if i > 0 {
			b.WriteByte('\n')
			b.WriteString(separator)
			b.WriteByte('\n')
		}
		b.WriteString(block)
	}
	return b.String()
}

// padViewportContentHeight ensures that content, which is the proposed contents of the viewport, has enough height, by adding rows of spaces with a bg color. This is nececessary so that
// the whole message area has the same bg color to the user, even if there's not much actual content in it.
func (m *model) padViewportContentHeight(content string, targetHeight int, width int) string {
	currentHeight := termformat.BlockHeight(content)

	debugLogf("current height: %v; targetHeight: %v, width: %v\n", currentHeight, targetHeight, width)
	if currentHeight >= targetHeight {
		return content
	}

	missing := targetHeight - currentHeight
	paddingRow := m.blankRow(width, m.palette.primaryBackground)

	var b strings.Builder
	if content != "" {
		b.WriteString(content)
		b.WriteByte('\n')
	}
	for i := 0; i < missing; i++ {
		b.WriteString(paddingRow)
		if i < missing-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func (m *model) blankRow(width int, background termformat.Color) string {
	if width <= 0 {
		width = 1
	}
	return termformat.Style{Background: background}.Wrap(strings.Repeat(" ", width))
}

func (m *model) updateTextareaHeight() {
	contents := ""
	if m.textarea != nil {
		contents = m.textarea.Contents()
	}
	lines := strings.Count(contents, "\n") + 1
	height := clamp(lines, minInputLines, maxInputLines)
	newHeight := height + 1 // 1: margin top

	if newHeight == m.textAreaHeight {
		return
	}

	wasAtBottom := false
	if m.viewport != nil {
		wasAtBottom = m.viewport.AtBottom()
	}
	m.textAreaHeight = newHeight
	m.updateSizes()
	m.refreshViewport(false) // need to refresh viewport because it may have too many blank lines of padding now

	// if text area high increases, that makes the viewport smaller. By default, that caused the last part of the content to be cut off. So this keep the content at bottom.
	if wasAtBottom && m.viewport != nil {
		m.viewport.ScrollToBottom()
	}
}

func (m *model) infoLineView() string {
	hints := []string{"ctrl-c to quit", "esc to stop agent", "ctrl-j for newline"}
	infoLineText := "  "

	for i, h := range hints {
		newInfoLineText := infoLineText
		if i == 0 {
			newInfoLineText += h
		} else {
			newInfoLineText += "  |  " + h
		}

		if len(newInfoLineText) >= m.viewportWidth {
			break
		}
		infoLineText = newInfoLineText
	}
	infoLine := termformat.Style{Foreground: m.palette.accentForeground}.Wrap(infoLineText)
	infoLine = termformat.BlockStyle{TotalWidth: m.viewportWidth, TextBackground: m.palette.primaryBackground}.Apply(infoLine)
	// debugLogf("%s", escapeForLog(infoLine))
	return infoLine
}

func (m *model) infoPanelBlock() string {
	body := m.infoPanelContent()
	if strings.TrimSpace(body) == "" {
		body = "info panel"
	}
	content := termformat.Style{Foreground: m.palette.primaryForeground}.Wrap(body)
	return termformat.BlockStyle{
		TotalWidth:        m.infoPanelWidth,
		TextBackground:    m.palette.accentBackground,
		BorderStyle:       termformat.BorderStyleThick,
		Padding:           1,
		BorderForeground:  m.palette.borderColor,
		BorderBackground:  m.palette.primaryBackground,
		PaddingBackground: m.palette.accentBackground,
		MinTotalHeight:    m.windowHeight,
	}.Apply(content)
}

func (m *model) infoPanelContent() string {
	var sections []string
	if usageSection := m.tokensCostSection(); usageSection != "" {
		sections = append(sections, usageSection)
	}
	if pkgSection := m.packageSection(); pkgSection != "" {
		sections = append(sections, pkgSection)
	}
	return strings.Join(sections, "\n\n")
}

func (m *model) tokensCostSection() string {
	sessionID := "<none>"
	modelName := string(defaultModelID)
	if m != nil && m.session != nil {
		if id := strings.TrimSpace(m.session.ID()); id != "" {
			sessionID = id
		}
		if name := strings.TrimSpace(m.session.ModelName()); name != "" {
			modelName = name
		}
	}

	info := m.currentModelInfo()
	var (
		usage          llmstream.TokenUsage
		contextPercent = -1
	)
	if agentInstance := m.currentAgent(); agentInstance != nil {
		usage = agentInstance.TokenUsage()
		contextPercent = agentInstance.ContextUsagePercent()
	}
	lines := make([]string, 0, 4)
	lines = append(lines,
		termformat.Sanitize(fmt.Sprintf("Session: %s", sessionID), 4),
		termformat.Sanitize(fmt.Sprintf("Model: %s", modelName), 4),
	)
	lines = append(lines, tokensCostLines(info, usage, contextPercent)...)

	return strings.Join(lines, "\n")
}

func (m *model) packageSection() string {
	pkgPath := ""
	switch {
	case m != nil && m.session != nil && m.session.packagePath != "":
		pkgPath = m.session.packagePath
	case m != nil:
		pkgPath = m.sessionConfig.packagePath
	}

	pkgPath = strings.TrimSpace(pkgPath)
	if pkgPath == "" {
		return "Package: <none>\nUse `/package path/to/pkg` to select a package."
	}

	return fmt.Sprintf("Package: %s", pkgPath)
}

func (m *model) currentModelInfo() llmmodel.ModelInfo {
	modelID := defaultModelID
	if m != nil && m.session != nil && m.session.modelID != "" {
		modelID = m.session.modelID
	}
	return llmmodel.GetModelInfo(modelID)
}

func (m *model) currentAgent() *agent.Agent {
	if m == nil || m.session == nil {
		return nil
	}
	return m.session.agent
}

func (m *model) permissionView() string {
	return m.permissionViewText
}

func (m *model) refreshPermissionView() {
	if m.activePermission == nil {
		m.permissionViewText = ""
		m.permissionViewHeight = 0
		m.updateSizes()
		return
	}

	req := m.activePermission.request
	var b strings.Builder
	b.WriteString(req.Prompt)
	b.WriteString("\n\n")
	b.WriteString("Y    allow\n")
	b.WriteString("N    deny\n")
	b.WriteString("ESC  deny + stop agent")

	rendered := termformat.BlockStyle{
		MarginLeft:         1,
		MarginRight:        1,
		TotalWidth:         m.viewportWidth,
		TextBackground:     m.palette.accentBackground,
		MarginBackground:   m.palette.primaryBackground,
		BlockNormalizeMode: termformat.BlockNormalizeModeExtend,
	}.Apply(b.String())

	m.permissionViewText = rendered
	m.permissionViewHeight = strings.Count(rendered, "\n") + 1
	m.updateSizes()
}

func (m *model) isAgentRunning() bool {
	return m.currentRun != nil
}

func (m *model) packageContextPending() bool {
	return m.packageContext != nil && m.packageContext.status == packageContextStatusPending
}

func (m *model) updatePlaceholder() {
	if m.textarea == nil {
		return
	}
	if m.isAgentRunning() {
		m.textarea.Placeholder = "Agent running... (ESC to stop)"
	} else {
		m.textarea.Placeholder = "Ready"
	}
}

func (m *model) enqueuePermissionRequest(req authdomain.UserRequest) {
	prompt := &permissionPrompt{request: req}
	if m.activePermission == nil {
		m.activePermission = prompt
		m.refreshPermissionView()
		return
	}
	m.permissionQueue = append(m.permissionQueue, prompt)
}

func (m *model) triggerPermissionDemo() {
	if m.activePermission != nil || len(m.permissionQueue) > 0 {
		m.appendSystemMessage("Resolve pending permissions before starting /permission.")
		m.refreshViewport(true)
		if m.viewport != nil {
			m.viewport.ScrollToBottom()
		}
		return
	}

	var (
		mu       sync.Mutex
		resolved bool
	)

	markResolved := func(message string) {
		mu.Lock()
		if resolved {
			mu.Unlock()
			return
		}
		resolved = true
		mu.Unlock()
		m.appendSystemMessage(message)
		m.refreshViewport(true)
		if m.viewport != nil {
			m.viewport.ScrollToBottom()
		}
	}

	req := authdomain.UserRequest{
		ToolName: "demo-permission",
		Argv:     []string{"echo", "permission-demo"},
		Prompt:   "Allow the demo permission request?",
		Allow: func() {
			markResolved("Demo permission granted.")
		},
		Disallow: func() {
			markResolved("Demo permission denied.")
		},
	}

	m.enqueuePermissionRequest(req)
	m.refreshPermissionView()
	m.refreshViewport(true)
	if m.viewport != nil {
		m.viewport.ScrollToBottom()
	}
}

func (m *model) advancePermissionQueue() {
	if len(m.permissionQueue) == 0 {
		m.activePermission = nil
		m.refreshPermissionView()
		return
	}
	next := m.permissionQueue[0]
	m.permissionQueue = m.permissionQueue[1:]
	m.activePermission = next
	m.refreshPermissionView()
}

func (m *model) rejectOutstandingPermissions() {
	if m.activePermission != nil {
		m.activePermission.request.Disallow()
		m.activePermission = nil
	}
	for _, queued := range m.permissionQueue {
		queued.request.Disallow()
	}
	m.permissionQueue = nil
	m.refreshPermissionView()
}

func (m *model) resetSession() {
	m.resetSessionWithConfig(m.sessionConfig)
}

func (m *model) startPackageContextGather() {
	if m == nil || m.session == nil || !m.session.config.packageMode() {
		m.packageContext = nil
		return
	}

	pkgPath := strings.TrimSpace(m.session.packagePath)
	if pkgPath == "" {
		m.packageContext = nil
		return
	}

	m.nextPackageContextID++
	runID := m.nextPackageContextID
	text := fmt.Sprintf("Gathering context for %s", pkgPath)
	index := m.appendContextStatusMessage(text, packageContextStatusPending)
	m.packageContext = &packageContextState{
		runID:        runID,
		messageIndex: index,
		status:       packageContextStatusPending,
		packagePath:  pkgPath,
	}

	if m.tui == nil {
		return
	}

	sandboxDir := m.session.sandboxDir
	pkgAbsPath := m.session.packageAbsPath
	go func() {
		contextText, err := buildPackageInitialContext(sandboxDir, pkgPath, pkgAbsPath)
		status := packageContextStatusSuccess
		if err != nil {
			status = packageContextStatusFailure
		}
		m.tui.Send(packageContextResultMsg{
			runID:  runID,
			status: status,
			text:   contextText,
		})
	}()
}

func (m *model) resetSessionWithConfig(cfg sessionConfig) {
	if m.sessionFactory == nil {
		m.appendSystemMessage("Failed to start new session: no session factory configured.")
		m.refreshViewport(true)
		return
	}

	nextSession, err := m.sessionFactory(cfg)
	if err != nil {
		m.appendSystemMessage(fmt.Sprintf("Failed to start new session: %v", err))
		m.refreshViewport(true)
		return
	}
	if nextSession == nil {
		m.appendSystemMessage("Failed to start new session: factory returned nil session.")
		m.refreshViewport(true)
		return
	}

	if m.session != nil {
		m.rejectOutstandingPermissions()
		m.stopUserRequestListener()
		m.session.Close()
	}

	m.session = nextSession
	m.sessionConfig = nextSession.config
	m.requests = nextSession.UserRequests()
	m.requestSource++
	m.messages = []chatMessage{{kind: messageKindWelcome}}
	m.messageQueue = nil
	m.permissionQueue = nil
	m.activePermission = nil
	m.pendingSessionConfig = nil
	m.packageContext = nil
	m.exitEditingState()
	m.editedHistoryDrafts = nil
	if m.textarea != nil {
		m.textarea.SetContents("")
	}
	m.updateTextareaHeight()
	m.refreshPermissionView()
	m.updatePlaceholder()
	m.startPackageContextGather()
	m.refreshViewport(true)
	if m.requests != nil {
		m.startUserRequestListener(m.requestSource, m.requests)
	}
}

// newTextArea makes a new textarea.
func newTextArea() *tuicontrols.TextArea {
	ti := tuicontrols.NewTextArea(0, 0)
	ti.Placeholder = "Ready"
	ti.Prompt = "› "
	return ti
}

// formatStopwatchDuration returns a human-readable stopwatch string with hour, minute,
// and second units, clamping negative durations to zero and always showing seconds.
func formatStopwatchDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	totalSeconds := int(d / time.Second)
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60

	var parts []string
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
	}
	if minutes > 0 || hours > 0 {
		parts = append(parts, fmt.Sprintf("%dm", minutes))
	}
	parts = append(parts, fmt.Sprintf("%ds", seconds))
	return strings.Join(parts, " ")
}

func clamp(v, minVal, maxVal int) int {
	if v < minVal {
		return minVal
	}
	if v > maxVal {
		return maxVal
	}
	return v
}
