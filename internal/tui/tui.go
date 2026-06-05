package tui

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/agentformatter"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/q/cas"
	"github.com/codalotl/codalotl/internal/q/clipboard"
	"github.com/codalotl/codalotl/internal/q/remotemonitor"
	"github.com/codalotl/codalotl/internal/q/termformat"
	qtui "github.com/codalotl/codalotl/internal/q/tui"
	"github.com/codalotl/codalotl/internal/q/tui/tuicontrols"
	"github.com/codalotl/codalotl/internal/skills"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
)

const (
	minInputLines         = 3
	maxInputLines         = 10
	historyIndexNone      = -1
	mouseWheelScrollLines = 3 // mouseWheelScrollLines is the number of lines to scroll per wheel "click".
)

const providerSubscriptionModelMarker = "subscription"

// messageKind classifies a chat message for rendering and message-specific state.
type messageKind int

// Message kind values classify messages for rendering and message-specific behavior.
const (
	messageKindSystem        messageKind = iota // messageKindSystem marks a system or status message shown in the Messages Area.
	messageKindSkillsList                       // messageKindSkillsList marks the rendered output of the /skills command.
	messageKindWelcome                          // messageKindWelcome marks the new-session banner and mode-specific help text.
	messageKindUser                             // messageKindUser marks a user message that has been sent to the agent.
	messageKindQueuedUser                       // messageKindQueuedUser marks a user message displayed immediately but not yet sent.
	messageKindAgent                            // messageKindAgent marks an event emitted by the agent, such as assistant text or a tool event.
	messageKindContextStatus                    // messageKindContextStatus marks Package Mode initial-context gathering status and details.
)

// packageContextStatus identifies the state of an asynchronous package-mode context-gathering run. The zero value is packageContextStatusPending.
type packageContextStatus int

const (
	packageContextStatusPending packageContextStatus = iota
	packageContextStatusSuccess
	packageContextStatusFailure
)

// contextStatusLine describes the rendered status of an asynchronous package-context operation.
type contextStatusLine struct {
	text   string               // Text is the user-visible status message.
	status packageContextStatus // Status determines the operation state and rendered status color.
}

// packageContextState tracks an asynchronous package-mode context-gathering run.
type packageContextState struct {
	runID        int                  // Run ID identifies this gather attempt and rejects stale results.
	messageIndex int                  // Message index points to the context-status message in the message list.
	status       packageContextStatus // Status is the latest state of the gather attempt.
	packagePath  string               // Package path is the sandbox-relative package being analyzed.
}

// chatMessage represents one discrete item in the Messages Area, including its kind-specific payload, overlay metadata, and cached rendered text.
type chatMessage struct {
	kind                messageKind          // Kind classifies the message for rendering and behavior.
	userMessage         string               // the unstyled, unformatted message exactly as the user typed it (also unstyled system messages).
	event               agent.Event          // Event is the agent event rendered for messageKindAgent messages.
	toolCallID          string               // ToolCallID identifies the tool call associated with an agent message, tool result, or tool output.
	toolOutputs         []agent.Event        // ToolOutputs are display-only tool output events shown under the associated tool call in arrival order.
	contextStatus       *contextStatusLine   // ContextStatus is the rendered Package Mode context-gathering status for messageKindContextStatus messages.
	contextDetails      string               // contextDetails and contextError are only used for messageKindContextStatus, and are displayed in Overlay Mode > Details.
	contextError        string               // ContextError is the package-context error text shown in Overlay Mode Details for messageKindContextStatus messages.
	skillsList          []skills.Skill       // skillsList is only set for messageKindSkillsList.
	skillsIssues        string               // skillsIssues is only set for messageKindSkillsList and describes skill load/validation errors.
	toolSubagentDisplay *toolSubagentDisplay // ToolSubagentDisplay tracks stable subagent slots owned by a tool-call message.

	// The ANSI formatted string. Each formatted must have all styles attached to it. It must be the correct block width (all lines padded with spaces to equal width
	// of the viewport. Background colors must be set on this (if we're not in the uncolored palette). Resize events need to recalculate this.
	formatted string

	// FormattedWidth is the viewport width used to produce formatted.
	formattedWidth int
}

// agentRun tracks an active agent execution and its event stream.
type agentRun struct {
	cancel context.CancelFunc // Cancel stops the run context.
	events <-chan agent.Event // Events receives progress and completion events from the agent.
	id     int                // ID identifies this run so stale events can be ignored.
}

// permissionPrompt is a pending user authorization prompt shown or queued in the TUI.
type permissionPrompt struct {
	request authdomain.UserRequest // Request is the authorization request whose Allow or Disallow callback resolves the prompt.
}

// agentEventMsg delivers an agent event to the TUI update loop.
type agentEventMsg struct {
	event agent.Event // event is the agent event to handle.
	runID int         // runID identifies the agent run that emitted event.
}

// agentStreamClosedMsg reports that an agent event stream has closed.
type agentStreamClosedMsg struct {
	runID int // runID identifies the agent run that produced the stream.
}

// workingIndicatorTickMsg requests a refresh of the running-agent working indicator.
type workingIndicatorTickMsg struct{}

// userRequestMsg delivers a tool authorization request to the TUI update loop.
type userRequestMsg struct {
	request  authdomain.UserRequest // request is the permission prompt to present to the user.
	sourceID int                    // sourceID identifies the request listener that received the prompt.
}

// packageContextResultMsg carries the result of asynchronous package-context gathering.
type packageContextResultMsg struct {
	runID  int                  // Run ID correlates this result with the active gather attempt.
	status packageContextStatus // Status is the resulting state of the gather attempt.
	text   string               // Text is the generated package context.
	errMsg string               // Error message describes a gather failure.
}

// specConformanceResultMsg carries the result of an asynchronous SPEC.md conformance check.
type specConformanceResultMsg struct {
	runID    int    // Run ID correlates this result with the active check.
	found    bool   // Found reports whether conformance metadata was found.
	conforms bool   // Conforms reports whether the retrieved metadata indicates conformance.
	errMsg   string // Error message describes a check failure for debug logging.
}

// queuedMessageDest classifies the delivery target for a queued message.
type queuedMessageDest int

const (
	queuedMessageDestLocal queuedMessageDest = iota
	queuedMessageDestAgent
)

// queuedMessage is user text waiting to be delivered to the agent.
type queuedMessage struct {
	text string            // text is the user message to send.
	dest queuedMessageDest // dest records where the pending message is queued.
}

// toolDisplayScope records a tool call whose descendant subagent events are rendered under that tool.
type toolDisplayScope struct {
	call                  llmstream.ToolCall                      // Call is the tool invocation that opened the display scope.
	finalMessagePresenter llmstream.SubagentFinalMessagePresenter // FinalMessagePresenter optionally replaces or suppresses final subagent text in this scope.
}

// toolDisplayScopeRef identifies an active tool display scope by agent and stack index.
type toolDisplayScopeRef struct {
	agentID string // Agent ID selects the owning agent's active tool-scope stack.
	index   int    // Index selects the scope within the agent's active tool-scope stack.
}

// Run launches the TUI in an alternate screen buffer.
func Run() error {
	return RunWithConfig(Config{})
}

// RunWithConfig launches the TUI using the provided configuration.
func RunWithConfig(cfg Config) error {
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
	initialCfg := sessionConfig{
		modelID:   cfg.ModelID,
		lintSteps: cfg.LintSteps,
		autoYes:   cfg.AutoYes,
	}
	initialSession, err := newSession(initialCfg)
	if err != nil {
		return err
	}

	model := newModel(palette, agentFormatter, initialSession, initialCfg, newSession, cfg.PersistModelID, cfg.Monitor, cfg.CASDB)

	return qtui.RunTUI(model, qtui.Options{
		Input:       os.Stdin,
		Output:      os.Stdout,
		Framerate:   60,
		EnableMouse: true, // required for wheel scrolling / MouseEvent delivery
	})
}

// model stores the mutable state for the coding-agent TUI.
type model struct {
	ready bool // ready is set on the first window size event. Only render TUI when ready=true

	//
	// At any given time, the window height and window width are windowHeight/windowWidth. From this, we must calculate the height/width of all "controls"/"areas", so that they can be cell-perfect aligned.
	//

	// Window height is the current terminal height in cells.
	windowHeight int

	// Window width is the current terminal width in cells.
	windowWidth int

	// viewportWidth and infoPanelWidth must sum to windowWidth. Below the viewport is the text area. It must be the same width as the viewport.
	viewportWidth int

	infoPanelWidth       int // Info panel width is the width reserved for the right-side Info Panel in cells.
	viewportHeight       int // Viewport height is the height reserved for the scrollable Messages Area in cells.
	textAreaHeight       int // height of text area AND any border around it
	infoLineHeight       int // height of info area below the text area (ex: could show things like common hotkeys, context usage, etc)
	permissionViewHeight int // 0 if not shown (activePermission == nil)

	// Implied:
	//   - text area width == viewportWidth
	//   - permission view width == viewportWidth
	//   - info panel height == windowHeight

	messages                     []chatMessage                         // Messages are the discrete items rendered in the Messages Area.
	messageHistory               []string                              // Message history contains sent user messages eligible for Up/Down cycling.
	editedHistoryDrafts          map[int]string                        // Edited history drafts preserve unsent edits by history index during history navigation.
	cyclingMode                  bool                                  // Cycling mode is true while Up/Down is browsing previous user messages.
	cycleIndex                   int                                   // Cycle index is the current history index shown while cycling.
	editingHistoryIndex          int                                   // Editing history index is the history entry being edited, or historyIndexNone.
	viewport                     *tuicontrols.View                     // Viewport is the scrollable Messages Area control.
	textarea                     *tuicontrols.TextArea                 // Textarea is the user input control.
	tui                          *qtui.TUI                             // TUI is the running terminal UI runtime.
	session                      *session                              // Session is the active agent session, or nil before session startup.
	sessionConfig                sessionConfig                         // Session config is the normalized configuration for the active or next session.
	sessionFactory               func(sessionConfig) (*session, error) // Session factory constructs sessions during startup and reset.
	startAgentRunHook            func(string)                          // Start agent run hook overrides normal run startup, primarily for tests.
	agentFormatter               agentformatter.Formatter              // Agent formatter renders agent events for the Messages Area.
	queuedMessages               []queuedMessage                       // pending messages not yet sent (either queued into the agent or queued locally)
	currentRun                   *agentRun                             // Current run is the active agent execution, or nil when the agent is idle.
	runStartedAt                 time.Time                             // Run started at records when the active run began for the working indicator.
	nextAgentRunID               int                                   // Next agent run ID is assigned to the next run so stale events can be ignored.
	workingIndicatorAnimationPos int                                   // Working indicator animation position selects the current animation frame.
	workingIndicatorTickerCancel qtui.CancelFunc                       // Working indicator ticker cancel stops periodic working-indicator updates.

	// When `/new` is invoked mid-run we mark the reset as pending so the cleanup happens only after `agentStreamClosedMsg` fires; that way we don't tear down session
	// state while events are still draining from the agent.
	pendingSessionConfig *sessionConfig

	permissionQueue      []*permissionPrompt                     // Permission queue contains prompts waiting behind the active permission prompt.
	activePermission     *permissionPrompt                       // Active permission is the prompt currently shown to the user.
	permissionViewText   string                                  // Permission view text is the rendered text for the visible permission prompt.
	requests             <-chan authdomain.UserRequest           // Requests receives authorization prompts from the current session.
	requestSource        int                                     // Request source identifies the request listener so stale prompt messages can be ignored.
	requestCancel        context.CancelFunc                      // Request cancel stops the active authorization-request listener.
	palette              colorPalette                            // Palette is the resolved color palette used for rendering.
	persistModelID       func(newModelID llmmodel.ModelID) error // If set, /model persists the selected model ID back to the caller's config source.
	packageContext       *packageContextState                    // Package context tracks an asynchronous Package Mode context-gathering run.
	nextPackageContextID int                                     // Next package context ID is assigned to the next context-gathering run.

	// pendingPostResetMessage is appended as a system message immediately after a session reset. This is primarily used by slash commands that start a new session (ex:
	// /model) but still want to confirm what happened.
	pendingPostResetMessage string

	pendingPostResetUserMessage string                 // Pending post-reset user message is sent after the next session reset.
	pendingPostResetStartRun    bool                   // Pending post-reset start run starts the agent after reset, even without an initial user message.
	monitor                     *remotemonitor.Monitor // Monitor provides latest-version checks for the Info Panel.
	latestVersion               string                 // Latest version is the version reported by the remote monitor.
	versionCheckStarted         bool                   // Version check started prevents launching duplicate latest-version checks.
	casDB                       *cas.DB                // CAS DB enables optional CAS-backed metadata checks in the UI.
	specConformance             *specConformanceState  // Spec conformance tracks the asynchronous SPEC.md conformance check.
	nextSpecConformanceID       int                    // Next spec conformance ID is assigned to the next SPEC.md conformance check.
	overlayMode                 bool                   // Overlay Mode: show clickable UI affordances in the viewport (currently: per-message copy).
	overlayCopyFeedback         map[int]time.Time      // overlayCopyFeedback tracks transient "copied!" feedback per message index.
	overlayTargets              []overlayTarget        // overlayTargets are computed on each refreshViewport; they map viewport content rows to message indices for hit-testing.
	lastLeftClickAt             time.Time              // lastLeftClick* is used for best-effort double-click detection.
	lastLeftClickX              int                    // Last left click X records the column used for double-click detection.
	lastLeftClickY              int                    // Last left click Y records the row used for double-click detection.
	clipboardSetter             func(text string)      // clipboardSetter is injected from *qtui.TUI in Init/Update, but can be overridden in tests.
	osClipboardAvailable        func() bool            // OS clipboard integration (best-effort); separated for testability so unit tests don't mutate the real system clipboard.

	// OS clipboard write performs best-effort direct clipboard writes.
	osClipboardWrite func(text string) error

	now                    func() time.Time                        // now allows deterministic tests around transient UI state (ex: "copied!").
	detailsDialog          *detailsDialog                          // detailsDialog is a modal "Details" overlay, opened from Overlay Mode for tool calls and package context gathering.
	agentParents           map[string]string                       // Agent parents maps agent IDs to parent agent IDs for hierarchy routing.
	subagentLabels         map[string]string                       // Subagent labels stores labels announced by subagent-start events.
	activeToolScopes       map[string][]toolDisplayScope           // Active tool scopes records tool calls whose descendants render under that tool.
	toolSubagentDisplays   map[string]*toolSubagentDisplay         // Tool subagent displays tracks stable-slot displays by tool call ID.
	toolCompletionByCallID map[string]llmstream.CompletionBehavior // Tool completion by call ID caches result display behavior by tool call ID.
}

// newModel constructs an initialized TUI model from the supplied session, rendering dependencies, and optional integrations. If initialSession is non-nil, the model
// adopts its normalized config, records its authorization request channel, and starts with a welcome message.
func newModel(
	palette colorPalette,
	formatter agentformatter.Formatter,
	initialSession *session,
	initialCfg sessionConfig,
	factory func(sessionConfig) (*session, error),
	persistModelID func(newModelID llmmodel.ModelID) error,
	monitor *remotemonitor.Monitor,
	casDB *cas.DB,
) *model {
	ti := newTextArea()
	vp := tuicontrols.NewView(0, 0)
	vp.SetEmptyLineBackgroundColor(palette.primaryBackground)

	activeCfg := initialCfg
	if initialSession != nil {
		activeCfg = initialSession.config
	}

	m := &model{
		viewport:               vp,
		textarea:               ti,
		session:                initialSession,
		sessionFactory:         factory,
		sessionConfig:          activeCfg,
		agentFormatter:         formatter,
		persistModelID:         persistModelID,
		requestSource:          1,
		nextAgentRunID:         1,
		messages:               make([]chatMessage, 0, 32),
		messageHistory:         make([]string, 0, 32),
		queuedMessages:         make([]queuedMessage, 0),
		permissionQueue:        make([]*permissionPrompt, 0),
		palette:                palette,
		cycleIndex:             historyIndexNone,
		editingHistoryIndex:    historyIndexNone,
		monitor:                monitor,
		casDB:                  casDB,
		now:                    time.Now,
		osClipboardAvailable:   clipboard.Available,
		osClipboardWrite:       clipboard.Write,
		agentParents:           make(map[string]string),
		subagentLabels:         make(map[string]string),
		activeToolScopes:       make(map[string][]toolDisplayScope),
		toolSubagentDisplays:   make(map[string]*toolSubagentDisplay),
		toolCompletionByCallID: make(map[string]llmstream.CompletionBehavior),
	}
	if initialSession != nil {
		m.requests = initialSession.UserRequests()
		m.messages = append(m.messages, chatMessage{kind: messageKindWelcome})
	}
	m.updatePlaceholder()
	return m
}

// Init attaches the model to the running TUI and starts session-scoped background listeners and checks. It preserves any injected clipboard setter, otherwise using
// the TUI's OSC52 clipboard integration when available.
func (m *model) Init(t *qtui.TUI) {
	m.tui = t
	if m.clipboardSetter == nil && t != nil {
		m.clipboardSetter = t.SetClipboard
	}
	if m.requests != nil {
		m.startUserRequestListener(m.requestSource, m.requests)
	}
	m.startLatestVersionCheck()
	m.startSpecConformanceCheck()
}

// Update applies a runtime message to the TUI model. It is the central event loop for user input, terminal events, agent events, permission prompts, session resets,
// and asynchronous UI updates.
func (m *model) Update(t *qtui.TUI, msg qtui.Message) {
	if m.tui == nil && t != nil {
		m.tui = t
	}
	if m.clipboardSetter == nil && t != nil {
		m.clipboardSetter = t.SetClipboard
	}

	switch ev := msg.(type) {
	case qtui.KeyEvent:
		skipTextarea := m.handleKeyEvent(ev)
		if !skipTextarea && m.textarea != nil {
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
			postResetMsg := m.pendingPostResetMessage
			m.pendingPostResetMessage = ""
			postResetUserMsg := m.pendingPostResetUserMessage
			m.pendingPostResetUserMessage = ""
			postResetStartRun := m.pendingPostResetStartRun
			m.pendingPostResetStartRun = false
			m.finishAgentRun()
			if pendingCfg != nil {
				m.resetSessionWithConfig(*pendingCfg)
				m.runPostResetActions(postResetMsg, postResetUserMsg, postResetStartRun)
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
	case latestVersionMsg:
		if ev.err != nil {
			debugLogf("latest version check error: %v", ev.err)
			break
		}
		m.latestVersion = ev.latest
	case specConformanceResultMsg:
		m.handleSpecConformanceResult(ev)
	case overlayCopyExpiredMsg:
		m.clearExpiredOverlayCopyFeedback()
		m.refreshViewport(false)
	}

	m.persistEditedHistoryDraft()
	m.updateTextareaHeight()
}

// View renders the current TUI frame. It returns placeholder text until the terminal is ready or large enough, and otherwise composes the message viewport, input
// areas, optional info panel, and any open modal.
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
		base := b.String()
		if m.detailsDialog != nil {
			return m.detailsDialogView(base)
		}
		return base
	}

	infoBlock := m.infoPanelBlock()

	blocks := []termformat.LayoutBlock{
		{Block: b.String(), X: 0, Y: 0},
		{Block: infoBlock, X: m.viewportWidth, Y: 0},
	}
	combo, err := termformat.Layout(blocks, nil)
	if err == nil {
		debugLogf("h=%d lines=%d rectHeight=%d", m.windowHeight, len(strings.Split(combo, "\n")), termformat.BlockHeight(combo))
		if m.detailsDialog != nil {
			return m.detailsDialogView(combo)
		}
		return combo
	} else {
		return fmt.Sprintf("rendering error: %v", err)
	}
}

// handleWindowSize applies a resize event and recalculates the TUI layout.
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

// HandleMouseEvent applies mouse input to the active TUI view. Wheel events scroll the details dialog when one is open, otherwise they scroll the messages viewport.
func (m *model) handleMouseEvent(ev qtui.MouseEvent) {
	if m.viewport == nil {
		return
	}

	if m.detailsDialog != nil {
		// When a modal dialog is up, mouse interactions apply to it (or are ignored).
		if ev.IsWheel() {
			switch ev.Button {
			case qtui.MouseButtonWheelUp:
				m.detailsDialogScrollUp(mouseWheelScrollLines)
			case qtui.MouseButtonWheelDown:
				m.detailsDialogScrollDown(mouseWheelScrollLines)
			}
		}
		return
	}

	// Wheel always scrolls the messages viewport.
	if ev.IsWheel() {
		switch ev.Button {
		case qtui.MouseButtonWheelUp:
			m.viewport.ScrollUp(mouseWheelScrollLines)
		case qtui.MouseButtonWheelDown:
			m.viewport.ScrollDown(mouseWheelScrollLines)
		}
		return
	}

	// Click handling (Overlay Mode, double-click to toggle).
	if ev.Action != qtui.MouseActionPress || ev.Button != qtui.MouseButtonLeft {
		return
	}

	// In Overlay Mode, clicks in the viewport can hit targets like "copy".
	if m.overlayMode && m.tryHandleOverlayClick(ev) {
		return
	}

	// Best-effort double-click toggles Overlay Mode.
	if m.isDoubleClick(ev) {
		m.toggleOverlayMode()
		return
	}

	m.lastLeftClickAt = m.nowOrTimeNow()
	m.lastLeftClickX = ev.X
	m.lastLeftClickY = ev.Y
}

// updateSizes calculates all sizes (dimensions) on m based on m.windowHeight and m.windowWidth. It updates fields on m (ex: m.viewportWidth), and also dimensions
// on any "components" we're using. This method is cheap to call, and can be called idempotently.
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

// viewportInfoPanelWidths returns the viewport width (messages area - left) and info panel width (right area). If the terminal is too small, don't show the info
// panel area (width=0).
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

// handleKeyEvent handles a key event that belongs to the TUI shell and reports whether it consumed the event.
//
// A false return means the caller should forward the key to the text area. The method gives modal dialogs and permission prompts exclusive handling, applies global
// shortcuts, submits messages, and manages history navigation.
func (m *model) handleKeyEvent(key qtui.KeyEvent) (skipTextarea bool) {
	if key.ControlKey == qtui.ControlKeyCtrlC {
		// Ctrl-C is "stop agent" when the agent is currently running; otherwise it quits
		// the app (keeping the bottom help text intact as-is).
		if m.isAgentRunning() {
			m.stopAgentRun()
			m.restoreQueuedMessagesToInput()
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

	if m.detailsDialog != nil {
		switch key.ControlKey {
		case qtui.ControlKeyEsc:
			m.closeDetailsDialog()
			return true
		case qtui.ControlKeyPageUp, qtui.ControlKeyCtrlPageUp:
			m.detailsDialogPageUp()
			return true
		case qtui.ControlKeyPageDown, qtui.ControlKeyCtrlPageDown:
			m.detailsDialogPageDown()
			return true
		case qtui.ControlKeyHome, qtui.ControlKeyCtrlHome, qtui.ControlKeyShiftHome, qtui.ControlKeyCtrlShiftHome:
			m.detailsDialogScrollToTop()
			return true
		case qtui.ControlKeyEnd, qtui.ControlKeyCtrlEnd, qtui.ControlKeyShiftEnd, qtui.ControlKeyCtrlShiftEnd:
			m.detailsDialogScrollToBottom()
			return true
		default:
			// Swallow other key presses so the modal doesn't mutate the main UI.
			return true
		}
	}

	if m.activePermission != nil {
		return m.handlePermissionKey(key)
	}

	if m.cyclingMode && m.shouldExitCyclingForKey(key) {
		m.exitCyclingModeForEditing()
	}

	switch key.ControlKey {
	case qtui.ControlKeyCtrlO:
		m.toggleOverlayMode()
		return true
	// Spec: these keys scroll the message area (viewport), not the text area.
	case qtui.ControlKeyPageUp, qtui.ControlKeyCtrlPageUp:
		if m.viewport != nil {
			m.viewport.PageUp()
		}
		return true
	case qtui.ControlKeyPageDown, qtui.ControlKeyCtrlPageDown:
		if m.viewport != nil {
			m.viewport.PageDown()
		}
		return true
	case qtui.ControlKeyHome, qtui.ControlKeyCtrlHome, qtui.ControlKeyShiftHome, qtui.ControlKeyCtrlShiftHome:
		if m.viewport != nil {
			m.viewport.ScrollToTop()
		}
		return true
	case qtui.ControlKeyEnd, qtui.ControlKeyCtrlEnd, qtui.ControlKeyShiftEnd, qtui.ControlKeyCtrlShiftEnd:
		if m.viewport != nil {
			m.viewport.ScrollToBottom()
		}
		return true
	case qtui.ControlKeyEsc:
		// ESC has a "clear input" behavior when the user has typed anything. This takes
		// precedence over other ESC behaviors (stopping the agent, cycling/edit modes).
		if m.textarea != nil && m.textarea.Contents() != "" {
			m.exitEditingState()
			m.textarea.SetContents("")
			m.updateTextareaHeight()
			return true
		}
		if m.cyclingMode {
			m.exitCyclingModeToDefault()
			return true
		}
		if m.isEditingHistory() {
			// Spec: when editing a previous message (not cycling), ESC exits the edit state
			// and clears the input (it does not re-enter cycling mode).
			m.exitEditingState()
			if m.textarea != nil {
				m.textarea.SetContents("")
				m.updateTextareaHeight()
			}
			return true
		}
		if m.isAgentRunning() {
			m.stopAgentRun()
			m.restoreQueuedMessagesToInput()
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

// HandleSlashCommand dispatches a slash command and updates the TUI state for the command result. It returns true when the command input has been consumed.
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
		m.pendingPostResetMessage = ""
		m.pendingPostResetUserMessage = ""
		m.pendingPostResetStartRun = false
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
	case "/model":
		modelArg := strings.TrimSpace(strings.TrimPrefix(cmd, "/model"))
		m.handleModelCommand(modelArg)
		return true
	case "/models":
		// `/models` is a read-only alias for `/model` (with no args). It intentionally
		// does not accept a model parameter.
		modelsArg := strings.TrimSpace(strings.TrimPrefix(cmd, "/models"))
		if modelsArg != "" {
			m.appendSystemMessage("Usage: `/models` (lists available models). Use `/model <id>` to switch models.")
			m.refreshViewport(true)
			if m.viewport != nil {
				m.viewport.ScrollToBottom()
			}
			return true
		}
		m.handleModelCommand("")
		return true
	case "/skills":
		skillsArg := strings.TrimSpace(strings.TrimPrefix(cmd, "/skills"))
		if skillsArg != "" {
			m.appendSystemMessage("Usage: `/skills` (lists installed skills).")
			m.refreshViewport(true)
			if m.viewport != nil {
				m.viewport.ScrollToBottom()
			}
			return true
		}
		m.handleSkillsCommand()
		return true
	case "/package":
		packageArg := strings.TrimSpace(strings.TrimPrefix(cmd, "/package"))
		m.handlePackageCommand(packageArg)
		return true
	case "/generic":
		m.handleGenericCommand()
		return true
	case "/orchestrate":
		orchestrateArg := strings.TrimSpace(strings.TrimPrefix(cmd, "/orchestrate"))
		m.handleOrchestrateCommand(orchestrateArg)
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

// handleNewSessionCommand handles /new by requesting a fresh session with the current configuration. It preserves the current mode and displays a stopping message
// when an active run must be canceled first.
func (m *model) handleNewSessionCommand() {
	cfg := m.sessionConfig
	message := ""
	if m.isAgentRunning() {
		message = "Stopping current task before starting a new session..."
	}
	m.requestSessionReset(cfg, message)
}

// handleGenericCommand handles /generic by requesting a new Generic Mode session. It clears Package Mode and any specialized agent selection while preserving the
// remaining session configuration.
func (m *model) handleGenericCommand() {
	cfg := m.sessionConfig
	cfg.packagePath = ""
	cfg.agentName = ""
	m.requestSessionReset(cfg, "Generic mode enabled. Use `/package path/to/pkg` (path relative to sandbox) to select a package.")
}

// handleOrchestrateCommand handles /orchestrate by starting a fresh Generic Mode pr-orchestrator session. arg is used as the initial user message when non-blank;
// otherwise the orchestrator run starts with no initial message.
func (m *model) handleOrchestrateCommand(arg string) {
	cfg := m.sessionConfig
	cfg.packagePath = ""
	cfg.agentName = orchestrateAgentName
	m.requestSessionResetWithFollowUp(cfg, "", "", arg, true)
}

// handleModelCommand handles `/model` command input. With an empty argument it lists the current and callable models; with one model ID it validates the ID, persists
// it best-effort when configured, and starts a new session using that model.
func (m *model) handleModelCommand(arg string) {
	arg = strings.TrimSpace(arg)

	// `/model` (no args): list models + usage help.
	if arg == "" {
		currentID := m.currentModelID()
		current := string(currentID)

		// Only list models that are callable in the current environment.
		available := llmmodel.AvailableModelIDsWithAuth()

		var b strings.Builder
		fmt.Fprintf(&b, "Current model: %s\n", formatModelIDWithAuthMarkers(currentID))
		b.WriteString("Available models:\n")
		if len(available) == 0 {
			b.WriteString("• <none> (no configured API keys or provider subscription logins found)\n")
			envVars := llmmodel.ProviderKeyEnvVars()
			if len(envVars) > 0 {
				var keys []string
				for _, pid := range llmmodel.AllProviderIDs {
					if key := strings.TrimSpace(envVars[pid]); key != "" {
						keys = append(keys, key)
					}
				}
				if len(keys) > 0 {
					b.WriteString("Set an API key env var (ex: ")
					b.WriteString(strings.Join(keys, ", "))
					b.WriteString(") or log in with a supported provider subscription (ex: `codalotl auth openai login`) and restart.\n")
				}
			}
		} else {
			for _, id := range available {
				line := "• " + string(id)
				var markers []string
				if string(id) == current {
					markers = append(markers, "current")
				}
				if llmmodel.ModelUsesProviderSubscription(id) {
					markers = append(markers, providerSubscriptionModelMarker)
				}
				if len(markers) > 0 {
					line += " (" + strings.Join(markers, ", ") + ")"
				}
				b.WriteString(line)
				b.WriteByte('\n')
			}
		}
		b.WriteString("Use `/model <id>` to switch models (starts a new session).")

		m.appendSystemMessage(strings.TrimSpace(b.String()))
		m.refreshViewport(true)
		if m.viewport != nil {
			m.viewport.ScrollToBottom()
		}
		return
	}

	fields := strings.Fields(arg)
	if len(fields) != 1 {
		m.appendSystemMessage("Usage: `/model <id>` (or `/model` to list available models).")
		m.refreshViewport(true)
		if m.viewport != nil {
			m.viewport.ScrollToBottom()
		}
		return
	}

	modelID := llmmodel.ModelID(strings.TrimSpace(fields[0]))
	if modelID == "" {
		m.appendSystemMessage("Usage: `/model <id>` (or `/model` to list available models).")
		m.refreshViewport(true)
		if m.viewport != nil {
			m.viewport.ScrollToBottom()
		}
		return
	}

	if !modelID.Valid() {
		var matches []llmmodel.ModelID
		for _, id := range llmmodel.AvailableModelIDs() {
			if strings.Contains(string(id), string(modelID)) {
				matches = append(matches, id)
			}
		}
		if len(matches) == 1 {
			modelID = matches[0]
		} else {
			m.appendSystemMessage(fmt.Sprintf("Unknown model %q. Use `/model` to list available models.", fields[0]))
			m.refreshViewport(true)
			if m.viewport != nil {
				m.viewport.ScrollToBottom()
			}
			return
		}
	}

	current := defaultModelID
	switch {
	case m != nil && m.session != nil && m.session.modelID != "":
		current = m.session.modelID
	case m != nil && m.sessionConfig.modelID != "":
		current = m.sessionConfig.modelID
	}

	if current == modelID {
		m.appendSystemMessage(fmt.Sprintf("Model is already %s.", modelID))
		m.refreshViewport(true)
		if m.viewport != nil {
			m.viewport.ScrollToBottom()
		}
		return
	}

	if m.persistModelID != nil {
		if err := m.persistModelID(modelID); err != nil {
			// Best effort: still switch for this session.
			m.appendSystemMessage(fmt.Sprintf("Failed to persist model %s: %v", modelID, err))
			m.refreshViewport(true)
			if m.viewport != nil {
				m.viewport.ScrollToBottom()
			}
		}
	}

	cfg := m.sessionConfig
	cfg.modelID = modelID
	cfg, err := m.normalizeConfigForCurrentSandbox(cfg)
	if err != nil {
		m.appendSystemMessage(fmt.Sprintf("Cannot switch model: %v", err))
		m.refreshViewport(true)
		if m.viewport != nil {
			m.viewport.ScrollToBottom()
		}
		return
	}

	m.requestSessionResetWithPostMessage(
		cfg,
		fmt.Sprintf("Switching model to %s...", modelID),
		fmt.Sprintf("Model set to %s.", modelID),
	)
}

// handleSkillsCommand appends the current session's installed skills and skill discovery issues to the Messages Area. Valid skills are shown sorted by name; if
// no session is active, skills are discovered from the current working directory as a fallback.
func (m *model) handleSkillsCommand() {
	var available []skills.Skill
	var invalid []skills.Skill
	var failed []error
	var loadErr error
	switch {
	case m != nil && m.session != nil && len(m.session.availableSkills) > 0:
		available = append([]skills.Skill(nil), m.session.availableSkills...)
		invalid = append([]skills.Skill(nil), m.session.invalidSkills...)
		failed = append([]error(nil), m.session.failedSkillLoads...)
		loadErr = m.session.skillsLoadErr
	case m != nil && m.session != nil:
		available = nil
		invalid = append([]skills.Skill(nil), m.session.invalidSkills...)
		failed = append([]error(nil), m.session.failedSkillLoads...)
		loadErr = m.session.skillsLoadErr
	default:
		// Best-effort fallback: discover skills based on the current working directory.
		// In practice the TUI always has an active session.
		searchDirs := skills.SearchPaths("")
		valid, invalidSkills, failedSkillLoads, err := skills.LoadSkills(searchDirs)
		available = valid
		invalid = invalidSkills
		failed = failedSkillLoads
		loadErr = err
	}

	sort.Slice(available, func(i, j int) bool {
		return available[i].Name < available[j].Name
	})

	issues := formatSkillsIssues(invalid, failed, loadErr)

	m.messages = append(m.messages, chatMessage{
		kind:         messageKindSkillsList,
		skillsList:   available,
		skillsIssues: issues,
	})
	m.refreshViewport(true)
	if m.viewport != nil {
		m.viewport.ScrollToBottom()
	}
}

// formatSkillsIssues returns display text for skill discovery and validation issues.
func formatSkillsIssues(invalid []skills.Skill, failed []error, loadErr error) string {
	var parts []string
	if loadErr != nil {
		parts = append(parts, fmt.Sprintf("discovery error: %v", loadErr))
	}
	if len(invalid) > 0 || len(failed) > 0 {
		if msg := strings.TrimSpace(skills.FormatSkillErrors(invalid, failed)); msg != "" {
			parts = append(parts, msg)
		}
	}
	combined := strings.TrimSpace(strings.Join(parts, "\n"))
	if combined == "" {
		return ""
	}

	// Keep `/skills` installed-skill bullets stable (tests, readability) by ensuring
	// the issues section does not emit "• " bullet lines.
	lines := strings.Split(combined, "\n")
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "•") {
			rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "•"))
			if rest == "" {
				lines[i] = ""
			} else {
				lines[i] = "- " + rest
			}
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// handlePackageCommand handles /package by entering Package Mode for arg or exiting to Generic Mode when arg is blank. Non-blank paths are normalized relative to
// the current sandbox; validation failures are shown as system messages and do not reset the session. On success, it clears specialized agent selection and requests
// a new Package Mode session.
func (m *model) handlePackageCommand(arg string) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		m.handleGenericCommand()
		return
	}

	cfg := m.sessionConfig
	cfg.packagePath = arg
	cfg.agentName = ""
	cfg, err := m.normalizeConfigForCurrentSandbox(cfg)
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

// requestSessionReset requests a reset to cfg without any post-reset message or user message. If an agent run is active, message is displayed while the run is stopped
// and the reset is deferred until run events finish draining.
func (m *model) requestSessionReset(cfg sessionConfig, message string) {
	m.requestSessionResetWithFollowUp(cfg, message, "", "", false)
}

// requestSessionResetWithPostMessage requests a session reset and displays postResetMessage after the new session starts.
func (m *model) requestSessionResetWithPostMessage(cfg sessionConfig, message string, postResetMessage string) {
	m.requestSessionResetWithFollowUp(cfg, message, postResetMessage, "", false)
}

// requestSessionResetWithFollowUp requests a session reset using cfg and optional follow-up work. If an agent run is active, it leaves an existing pending reset
// unchanged; otherwise it cancels the run, rejects outstanding permissions, clears queued user messages, optionally shows message, and defers the reset until run
// events finish draining. After the new session starts, postResetMessage is displayed, postResetUserMessage is sent, and postResetStartRun starts the agent even
// when there is no message.
func (m *model) requestSessionResetWithFollowUp(cfg sessionConfig, message string, postResetMessage string, postResetUserMessage string, postResetStartRun bool) {
	message = strings.TrimSpace(message)
	postResetMessage = strings.TrimSpace(postResetMessage)
	postResetUserMessage = strings.TrimSpace(postResetUserMessage)

	if m.isAgentRunning() {
		if m.pendingSessionConfig != nil {
			return
		}
		pending := cfg
		m.pendingSessionConfig = &pending
		m.pendingPostResetMessage = postResetMessage
		m.pendingPostResetUserMessage = postResetUserMessage
		m.pendingPostResetStartRun = postResetStartRun
		m.queuedMessages = nil
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
	m.runPostResetActions(postResetMessage, postResetUserMessage, postResetStartRun)
}

// normalizeConfigForCurrentSandbox normalizes cfg using cfg.sandboxDir, the active session sandbox, or the current working directory.
func (m *model) normalizeConfigForCurrentSandbox(cfg sessionConfig) (sessionConfig, error) {
	sandboxDir := strings.TrimSpace(cfg.sandboxDir)
	if sandboxDir == "" {
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
	}

	normalizedCfg, _, err := normalizeSessionConfig(cfg, sandboxDir)
	normalizedCfg.sandboxDir = sandboxDir
	return normalizedCfg, err
}

// runPostResetActions performs queued follow-up work after a session reset.
func (m *model) runPostResetActions(postResetMessage string, postResetUserMessage string, postResetStartRun bool) {
	if postResetMessage != "" {
		m.appendSystemMessage(postResetMessage)
	}
	if postResetUserMessage != "" {
		m.sendOrQueueMessage(postResetUserMessage)
		m.startAgentRunIfPossible(postResetUserMessage)
	} else if postResetStartRun {
		m.startAgentRunIfPossible("")
	}
	if postResetMessage != "" || postResetUserMessage != "" {
		m.refreshViewport(true)
		if m.viewport != nil {
			m.viewport.ScrollToBottom()
		}
	}
}

// shouldExitCyclingForKey reports whether msg should turn the currently cycled history entry into an editable draft.
//
// It returns false when Cycling Mode is inactive or when msg is a cycling, submission, overlay, or viewport-navigation key.
func (m *model) shouldExitCyclingForKey(msg qtui.KeyEvent) bool {
	if !m.cyclingMode {
		return false
	}

	switch msg.ControlKey {
	case qtui.ControlKeyUp,
		qtui.ControlKeyDown,
		qtui.ControlKeyEsc,
		qtui.ControlKeyCtrlO,
		qtui.ControlKeyEnter,
		qtui.ControlKeyPageUp,
		qtui.ControlKeyPageDown,
		qtui.ControlKeyCtrlPageUp,
		qtui.ControlKeyCtrlPageDown,
		qtui.ControlKeyHome,
		qtui.ControlKeyEnd,
		qtui.ControlKeyCtrlHome,
		qtui.ControlKeyCtrlEnd,
		qtui.ControlKeyShiftHome,
		qtui.ControlKeyShiftEnd,
		qtui.ControlKeyCtrlShiftHome,
		qtui.ControlKeyCtrlShiftEnd:
		return false
	}

	return true
}

// handlePermissionKey resolves or swallows key input while a permission prompt is active.
func (m *model) handlePermissionKey(msg qtui.KeyEvent) bool {
	if m.activePermission == nil {
		return true
	}

	switch msg.ControlKey {
	case qtui.ControlKeyEsc:
		m.resolvePermission(false)
		m.stopAgentRun()
		m.restoreQueuedMessagesToInput()
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

// resolvePermission resolves the active permission prompt and advances to the next prompt.
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

// recordSubmittedMessage records value in message history if it is eligible for history cycling.
func (m *model) recordSubmittedMessage(value string) {
	m.exitEditingState()
	if len(m.editedHistoryDrafts) > 0 {
		m.editedHistoryDrafts = nil
	}
	if m.shouldSaveToHistory(value) {
		m.messageHistory = append(m.messageHistory, value)
	}
}

// shouldSaveToHistory reports whether value should be kept for Up/Down message-history cycling.
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
	case "/new", "/model", "/models", "/skills", "/quit", "/exit", "/logout":
		return false
	}
	return len(fields) > 1
}

// enterCyclingMode starts message-history cycling at the most recent saved message.
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

// cyclePrevious moves history navigation to the previous saved message.
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

// cycleNext moves history navigation to the next saved message or exits at the end.
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

// exitCyclingModeToDefault leaves history navigation and restores a blank input area.
func (m *model) exitCyclingModeToDefault() {
	m.exitEditingState()
	if m.textarea != nil {
		m.textarea.SetContents("")
	}
	m.updateTextareaHeight()
}

// exitCyclingModeForEditing leaves Cycling Mode and marks the current history entry as being edited.
func (m *model) exitCyclingModeForEditing() {
	if !m.cyclingMode {
		return
	}
	m.editingHistoryIndex = m.cycleIndex
	m.cyclingMode = false
	m.cycleIndex = historyIndexNone
}

// exitEditingState leaves history cycling and editing state without changing the text area.
func (m *model) exitEditingState() {
	m.cyclingMode = false
	m.cycleIndex = historyIndexNone
	m.editingHistoryIndex = historyIndexNone
}

// isEditingHistory reports whether a history entry is active for editing.
func (m *model) isEditingHistory() bool {
	return m.editingHistoryIndex != historyIndexNone
}

// showHistoryEntry loads the history entry at index into the text area for history navigation.
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

// historyValue returns the current editable value for the history entry at index.
func (m *model) historyValue(index int) string {
	if index < 0 || index >= len(m.messageHistory) {
		return ""
	}
	if edited, ok := m.editedHistoryDrafts[index]; ok {
		return edited
	}
	return m.messageHistory[index]
}

// persistEditedHistoryDraft saves the current text area contents for the active history entry.
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

// sendOrQueueMessage reflects value in the Messages Area and queues it for delivery when the agent can accept it.
func (m *model) sendOrQueueMessage(value string) {
	// Package context gathering can be in-flight during package-mode session start. In
	// that window, queue locally and send only after the context has been applied.
	if m.packageContextPending() {
		m.queuedMessages = append(m.queuedMessages, queuedMessage{text: value, dest: queuedMessageDestLocal})
		m.appendUserMessage(value, true)
		m.refreshViewport(true)
		if m.viewport != nil {
			m.viewport.ScrollToBottom()
		}
		return
	}
	if m.isAgentRunning() {
		// Spec: allow enqueuing messages mid-run so they can be injected at the agent's
		// next safe boundary (ex: after a tool result is appended).
		queuedToAgent := false
		if m.session != nil {
			if err := m.session.QueueUserMessage(value); err == nil {
				m.queuedMessages = append(m.queuedMessages, queuedMessage{text: value, dest: queuedMessageDestAgent})
				queuedToAgent = true
			}
		}
		if !queuedToAgent {
			// Fallback: if the agent is no longer accepting queued messages, queue
			// locally and send it once the current run finishes.
			m.queuedMessages = append(m.queuedMessages, queuedMessage{text: value, dest: queuedMessageDestLocal})
		}
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

// startAgentRunIfPossible starts an agent run unless session, run, or package-context state prevents it.
func (m *model) startAgentRunIfPossible(value string) {
	if m.session == nil || m.isAgentRunning() || m.packageContextPending() {
		return
	}
	if m.startAgentRunHook != nil {
		m.startAgentRunHook(value)
		return
	}
	m.startAgentRun(value)
}

// startAgentRun starts an agent turn for value and connects its event stream to the TUI.
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

// forwardAgentEvents forwards events from an agent run into the TUI update loop.
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

// stopAgentRun requests cancellation of the active agent run.
func (m *model) stopAgentRun() {
	if m.currentRun != nil {
		m.currentRun.cancel()
	}
}

// finishAgentRun clears active-run UI state after an agent event stream has ended.
func (m *model) finishAgentRun() {
	m.stopWorkingIndicatorTicker()
	m.currentRun = nil
	m.runStartedAt = time.Time{}
	m.workingIndicatorAnimationPos = 0
	m.resetToolDisplayState()
	m.updatePlaceholder()
	m.refreshViewport(m.shouldAutoScrollOnUpdate())
}

// startWorkingIndicatorTicker starts or replaces the periodic working-indicator refresh ticker.
func (m *model) startWorkingIndicatorTicker() {
	m.stopWorkingIndicatorTicker()
	if m.tui == nil {
		return
	}
	m.workingIndicatorTickerCancel = m.tui.SendPeriodically(workingIndicatorTickMsg{}, time.Second)
}

// stopWorkingIndicatorTicker stops periodic working-indicator updates. It is safe to call when the ticker is not running.
func (m *model) stopWorkingIndicatorTicker() {
	if m.workingIndicatorTickerCancel != nil {
		m.workingIndicatorTickerCancel()
		m.workingIndicatorTickerCancel = nil
	}
}

// startNextQueuedMessage sends the oldest queued user message when the session is ready.
func (m *model) startNextQueuedMessage() {
	if len(m.queuedMessages) == 0 || m.session == nil || m.packageContextPending() {
		return
	}

	next := m.queuedMessages[0].text
	m.queuedMessages = m.queuedMessages[1:]
	m.appendUserMessage(next, false)
	m.refreshViewport(true)
	if m.viewport != nil {
		m.viewport.ScrollToBottom()
	}
	m.startAgentRun(next)
}

// startUserRequestListener forwards authorization requests from requests into the TUI update loop with sourceID.
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

// stopUserRequestListener stops forwarding authorization requests into the TUI.
func (m *model) stopUserRequestListener() {
	if m.requestCancel != nil {
		m.requestCancel()
		m.requestCancel = nil
	}
}

// restoreQueuedMessagesToInput returns queued user messages to the text area after an interruption. It joins multiple queued messages with newlines, clears the
// queue, and updates the input height.
func (m *model) restoreQueuedMessagesToInput() {
	if len(m.queuedMessages) == 0 {
		return
	}
	m.exitEditingState()
	if m.textarea != nil {
		pending := make([]string, 0, len(m.queuedMessages))
		for _, msg := range m.queuedMessages {
			pending = append(pending, msg.text)
		}
		m.textarea.SetContents(strings.Join(pending, "\n"))
		m.textarea.MoveToEndOfText()
	}
	m.queuedMessages = nil
	m.updateTextareaHeight()
}

// appendUserMessage appends value as a sent or queued user message. Callers are responsible for refreshing the viewport after updating message state.
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

// appendSystemMessage appends value as a system message in the Messages Area.
func (m *model) appendSystemMessage(value string) {
	m.messages = append(m.messages, chatMessage{
		kind:        messageKindSystem,
		userMessage: value,
	})
}

// appendContextStatusMessage appends a context-status message and returns its message index.
func (m *model) appendContextStatusMessage(text string, status packageContextStatus) int {
	msg := chatMessage{
		kind:          messageKindContextStatus,
		userMessage:   text,
		contextStatus: &contextStatusLine{text: text, status: status},
	}
	m.messages = append(m.messages, msg)
	return len(m.messages) - 1
}

// updateContextStatusMessage changes a context-status message and invalidates its cached formatting. It does nothing when index is outside the message list.
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

// handleAgentEvent applies ev to the TUI message state and refreshes the viewport. It preserves manual scroll position, routes subagent and tool-display events,
// and renders user, assistant, tool, and status events according to the message-area rules.
func (m *model) handleAgentEvent(ev agent.Event) {
	m.recordAgentMeta(ev.Agent)
	m.recordSubagentStart(ev)
	if ev.Type == agent.EventTypeToolCall {
		m.beginToolDisplayScope(ev)
		m.startToolSubagentDisplay(ev)
	}
	if ev.Type == agent.EventTypeToolComplete {
		defer m.clearCompletedToolState(ev)
		defer m.endToolDisplayScope(ev)
	}

	autoScroll := m.shouldAutoScrollOnUpdate()
	if m.handleToolSubagentDescendantEvent(ev, autoScroll) {
		return
	}
	if m.handleDescendantSubagentFinalMessage(ev, autoScroll) {
		return
	}

	switch ev.Type {
	case agent.EventTypeUserMessageQueued:
		// Queued messages are already reflected in the UI immediately when the user
		// hits ENTER; do not print a separate agent event for them.
		return
	case agent.EventTypeQueuedUserMessageSent:
		// Spec: queued user messages are re-reflected when the agent actually appends
		// them into the conversation.
		if ev.UserMessage != "" {
			m.dropQueuedMessage(ev.UserMessage)
			m.appendUserMessage(ev.UserMessage, false)
			m.refreshViewport(autoScroll)
		}
		return
	}

	switch ev.Type {
	case agent.EventTypeAssistantTurnComplete:
		m.refreshViewport(autoScroll)
		return
	case agent.EventTypeStartSubagent:
		return
	case agent.EventTypeDoneSuccess:
		return
	}

	if ev.Type == agent.EventTypeToolComplete {
		if id := eventToolCallID(ev); id != "" && m.shouldReplaceToolCallWithResult(ev) && m.replaceToolEvent(id, ev) {
			m.refreshViewport(autoScroll)
			return
		}
	}
	if ev.Type == agent.EventTypeToolOutput {
		if m.appendToolOutput(ev) {
			m.refreshViewport(autoScroll)
		}
		return
	}

	m.appendAgentEvent(ev)
	m.refreshViewport(autoScroll)
}

// dropQueuedMessage removes the first queued user message matching message.
func (m *model) dropQueuedMessage(message string) {
	if len(m.queuedMessages) == 0 {
		return
	}
	for i, pending := range m.queuedMessages {
		if pending.text == message {
			copy(m.queuedMessages[i:], m.queuedMessages[i+1:])
			m.queuedMessages = m.queuedMessages[:len(m.queuedMessages)-1]
			return
		}
	}
}

// handlePackageContextResult applies the result of an asynchronous Package Mode context-gathering run. It updates the visible status and details, adds gathered
// context to the active agent, and starts queued work when the agent is idle.
func (m *model) handlePackageContextResult(msg packageContextResultMsg) {
	if m.packageContext == nil || m.packageContext.runID != msg.runID {
		return
	}

	m.packageContext.status = msg.status
	m.updateContextStatusMessage(m.packageContext.messageIndex, msg.status)
	if idx := m.packageContext.messageIndex; idx >= 0 && idx < len(m.messages) {
		m.messages[idx].contextDetails = msg.text
		m.messages[idx].contextError = msg.errMsg
	}

	if msg.text != "" && m.session != nil && m.session.agent != nil {
		if err := m.session.agent.AddUserTurn(msg.text); err != nil {
			m.appendSystemMessage(fmt.Sprintf("Failed to apply package context: %v", err))
			m.updateContextStatusMessage(m.packageContext.messageIndex, packageContextStatusFailure)
			if idx := m.packageContext.messageIndex; idx >= 0 && idx < len(m.messages) {
				m.messages[idx].contextError = err.Error()
			}
		}
	}

	if !m.isAgentRunning() {
		m.startNextQueuedMessage()
	}
	m.refreshViewport(true)
}

// shouldAutoScrollOnUpdate reports whether the viewport should stay pinned to the bottom. It preserves manual scroll position by returning true only when the viewport
// is already at the bottom.
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

// appendAgentEvent records ev as an agent message without refreshing the viewport.
func (m *model) appendAgentEvent(ev agent.Event) {
	toolCallID := eventToolCallID(ev)
	msg := chatMessage{
		kind:       messageKindAgent,
		event:      ev,
		toolCallID: toolCallID,
	}
	if ev.Type == agent.EventTypeToolCall {
		msg.toolSubagentDisplay = m.toolSubagentDisplays[toolCallID]
	}
	m.messages = append(m.messages, msg)
}

// appendToolOutput appends ev to its tool-call message and reports whether it was displayed.
func (m *model) appendToolOutput(ev agent.Event) bool {
	callID := eventToolCallID(ev)
	if callID == "" || !toolOutputVisible(ev) {
		return false
	}
	for i := len(m.messages) - 1; i >= 0; i-- {
		msg := &m.messages[i]
		if msg.kind != messageKindAgent || msg.toolCallID != callID || msg.event.Type != agent.EventTypeToolCall {
			continue
		}
		msg.toolOutputs = append(msg.toolOutputs, ev)
		msg.formatted = ""
		msg.formattedWidth = 0
		return true
	}
	return false
}

// replaceToolEvent replaces the displayed tool-call event for callID and reports whether it found one.
func (m *model) replaceToolEvent(callID string, ev agent.Event) bool {
	if callID == "" {
		return false
	}
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].toolCallID == callID && m.messages[i].event.Type == agent.EventTypeToolCall {
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
	if ev.Tool != nil {
		return ev.Tool.Name()
	}
	if ev.ToolCall != nil {
		return ev.ToolCall.Name
	}
	if ev.ToolResult != nil {
		return ev.ToolResult.Name
	}
	return ""
}

// shouldReplaceToolCallWithResult reports whether a tool result should replace its prior tool-call message.
func (m *model) shouldReplaceToolCallWithResult(ev agent.Event) bool {
	if callID := eventToolCallID(ev); callID != "" && m.toolCallHasVisibleOutput(callID) {
		return false
	}
	switch m.toolCompletionBehavior(ev) {
	case llmstream.CompletionBehaviorAppend:
		return false
	default:
		return true
	}
}

// toolCallHasVisibleOutput reports whether callID has visible tool output in the Messages Area.
func (m *model) toolCallHasVisibleOutput(callID string) bool {
	if callID == "" {
		return false
	}
	for i := len(m.messages) - 1; i >= 0; i-- {
		msg := &m.messages[i]
		if msg.kind == messageKindAgent && msg.toolCallID == callID && len(msg.toolOutputs) > 0 {
			return true
		}
	}
	return false
}

func toolOutputVisible(ev agent.Event) bool {
	return strings.TrimSpace(ev.ToolOutput.Content) != ""
}

// toolCompletionBehavior reports whether ev's tool result should replace or append to the tool-call presentation.
func (m *model) toolCompletionBehavior(ev agent.Event) llmstream.CompletionBehavior {
	if ev.Tool != nil && ev.ToolCall != nil {
		if presenter := ev.Tool.Presenter(); presenter != nil {
			var result *llmstream.ToolResult
			if ev.Type == agent.EventTypeToolComplete {
				result = ev.ToolResult
			}
			if behavior := presenter.Present(*ev.ToolCall, result).Behavior; behavior != "" {
				return behavior
			}
		}
	}
	if callID := eventToolCallID(ev); callID != "" {
		if behavior, ok := m.toolCompletionByCallID[callID]; ok {
			return behavior
		}
	}
	return llmstream.CompletionBehaviorReplace
}

// recordAgentMeta records the parent relationship for an agent event when the agent ID is present.
func (m *model) recordAgentMeta(meta agent.AgentMeta) {
	if meta.ID == "" {
		return
	}
	m.agentParents[meta.ID] = meta.Parent
}

// resetToolDisplayState clears all per-run tool and subagent display routing state.
func (m *model) resetToolDisplayState() {
	clear(m.agentParents)
	clear(m.subagentLabels)
	clear(m.activeToolScopes)
	clear(m.toolSubagentDisplays)
	clear(m.toolCompletionByCallID)
}

// beginToolDisplayScope records a tool call whose descendant subagent events may render under that call.
func (m *model) beginToolDisplayScope(ev agent.Event) {
	if ev.ToolCall == nil {
		return
	}
	m.toolCompletionByCallID[ev.ToolCall.CallID] = m.toolCompletionBehavior(ev)
	if ev.Agent.ID == "" {
		return
	}
	scope := toolDisplayScope{
		call: *ev.ToolCall,
	}
	if presenter, ok := toolSubagentFinalMessagePresenter(ev); ok {
		scope.finalMessagePresenter = presenter
	}
	m.activeToolScopes[ev.Agent.ID] = append(m.activeToolScopes[ev.Agent.ID], scope)
}

// clearCompletedToolState removes cached completion behavior for ev's tool call.
func (m *model) clearCompletedToolState(ev agent.Event) {
	callID := eventToolCallID(ev)
	if callID == "" {
		return
	}
	delete(m.toolCompletionByCallID, callID)
}

// endToolDisplayScope removes the completed tool call from active descendant-display routing.
func (m *model) endToolDisplayScope(ev agent.Event) {
	if ev.Agent.ID == "" {
		return
	}
	callID := eventToolCallID(ev)
	if callID == "" {
		return
	}
	scopes := m.activeToolScopes[ev.Agent.ID]
	for i := len(scopes) - 1; i >= 0; i-- {
		if scopes[i].call.CallID != callID {
			continue
		}
		scopes = append(scopes[:i], scopes[i+1:]...)
		if len(scopes) == 0 {
			delete(m.activeToolScopes, ev.Agent.ID)
		} else {
			m.activeToolScopes[ev.Agent.ID] = scopes
		}
		return
	}
}

func toolSubagentFinalMessagePresenter(ev agent.Event) (llmstream.SubagentFinalMessagePresenter, bool) {
	if ev.Tool == nil || ev.ToolCall == nil {
		return nil, false
	}
	presenter := ev.Tool.Presenter()
	if presenter == nil {
		return nil, false
	}
	finalMessagePresenter, ok := presenter.(llmstream.SubagentFinalMessagePresenter)
	return finalMessagePresenter, ok
}

// handleDescendantSubagentFinalMessage renders or suppresses final descendant subagent text with its enclosing tool presenter. It returns true when the event was
// consumed.
func (m *model) handleDescendantSubagentFinalMessage(ev agent.Event, autoScroll bool) bool {
	if ev.Type != agent.EventTypeAssistantText || !ev.AssistantTextFinalizing {
		return false
	}

	ref, ok := m.enclosingToolDisplayScope(ev.Agent)
	if !ok {
		return false
	}
	scope := m.toolDisplayScope(ref)
	if scope == nil || scope.finalMessagePresenter == nil {
		return false
	}

	return m.handleCustomizedDescendantFinalMessage(ref, ev, autoScroll)
}

// enclosingToolDisplayScope returns the nearest active tool display scope enclosing meta. It returns false when none of meta's ancestors has an active tool display
// scope.
func (m *model) enclosingToolDisplayScope(meta agent.AgentMeta) (toolDisplayScopeRef, bool) {
	for agentID := meta.Parent; agentID != ""; agentID = m.agentParents[agentID] {
		scopes := m.activeToolScopes[agentID]
		if len(scopes) == 0 {
			continue
		}
		return toolDisplayScopeRef{agentID: agentID, index: len(scopes) - 1}, true
	}
	return toolDisplayScopeRef{}, false
}

// toolDisplayScope returns the active tool display scope identified by ref. It returns nil when ref does not identify an existing scope.
func (m *model) toolDisplayScope(ref toolDisplayScopeRef) *toolDisplayScope {
	scopes := m.activeToolScopes[ref.agentID]
	if ref.index < 0 || ref.index >= len(scopes) {
		return nil
	}
	return &scopes[ref.index]
}

// handleCustomizedDescendantFinalMessage renders or suppresses a descendant subagent final message with the owning tool presenter.
func (m *model) handleCustomizedDescendantFinalMessage(ref toolDisplayScopeRef, ev agent.Event, autoScroll bool) bool {
	scope := m.toolDisplayScope(ref)
	if scope == nil {
		return false
	}

	block := scope.finalMessagePresenter.SubagentFinalMessage(
		scope.call,
		m.subagentLabels[ev.Agent.ID],
		ev.TextContent.Content,
	)
	if block == nil {
		return true
	}

	content := agentformatter.RenderPlainTextBlock(block)
	if content == "" {
		return true
	}

	m.appendAgentEvent(agent.Event{
		Agent:                   ev.Agent,
		Type:                    agent.EventTypeAssistantText,
		AssistantTextFinalizing: ev.AssistantTextFinalizing,
		TextContent: llmstream.TextContent{
			ProviderID: ev.TextContent.ProviderID,
			Content:    content,
		},
	})
	m.refreshViewport(autoScroll)
	return true
}

// recordSubagentStart records the label for a started subagent when ev is a subagent-start event.
func (m *model) recordSubagentStart(ev agent.Event) {
	if ev.Type != agent.EventTypeStartSubagent || ev.Agent.ID == "" {
		return
	}
	m.subagentLabels[ev.Agent.ID] = ev.StartSubagent.Label
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

	blocks := make([]renderedBlock, 0, len(m.messages)+2)
	for i := range m.messages {
		blocks = append(blocks, renderedBlock{
			text:         m.messages[i].formatted,
			messageIndex: i,
			copyable:     m.isMessageCopyable(&m.messages[i]),
			detailable:   m.isMessageDetailable(&m.messages[i]),
		})
	}
	if m.isAgentRunning() {
		if indicator := m.renderWorkingIndicator(width); indicator != "" {
			blocks = append(blocks, renderedBlock{text: indicator, messageIndex: -1, copyable: false})
		}
	}
	blocks = append(blocks, renderedBlock{text: m.blankRow(width, m.palette.primaryBackground), messageIndex: -1, copyable: false}) // always have one blank line at the end

	content, targets := m.joinRenderedBlocksWithOverlay(blocks, width)
	m.overlayTargets = targets
	content = m.padViewportContentHeight(content, height, width)
	if m.viewport != nil {
		m.viewport.SetContent(content)
		if autoScroll {
			m.viewport.ScrollToBottom()
		}
	}
}

// ensureMessageFormatted renders msg for width and caches the result in msg.formatted. msg must be non-nil. Widths less than one use agentformatter.MinTerminalWidth,
// and an existing cache for the same width is reused. The rendered block includes the palette styling, background, and padding needed for direct insertion into
// the Messages Area.
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
	case messageKindSkillsList:
		content = m.renderSkillsListMessage(msg.skillsList, msg.skillsIssues)
		needBgAndWidth = true
	case messageKindContextStatus:
		content = m.renderContextStatusLine(msg.contextStatus)
		needBgAndWidth = true
	case messageKindUser:
		content = m.renderUserMessageBlock(msg.userMessage, false, width)
	case messageKindQueuedUser:
		content = m.renderUserMessageBlock(msg.userMessage, true, width)
	case messageKindAgent:
		if custom, ok := m.renderToolSpecificMessage(msg, width); ok {
			content = custom
		} else {
			content = m.agentFormatter.FormatEvent(msg.event, width)
		}
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

// renderSkillsListMessage returns styled, unpadded display text for the `/skills` message. The available skills are rendered in their input order, unnamed skills
// are skipped, and names, descriptions, and issue lines are sanitized for terminal display.
func (m *model) renderSkillsListMessage(available []skills.Skill, issues string) string {
	primary := termformat.Style{Foreground: m.palette.primaryForeground}
	accent := termformat.Style{Foreground: m.palette.accentForeground}

	var b strings.Builder
	b.WriteString(primary.Wrap("Installed skills:"))
	b.WriteByte('\n')

	if len(available) == 0 {
		b.WriteString(primary.Wrap("• <none>"))
		return b.String()
	}

	wrote := 0
	for _, s := range available {
		name := strings.TrimSpace(s.Name)
		if name == "" {
			continue
		}
		// Names/descriptions come from local files; still sanitize to avoid control
		// codes and normalize newlines to one-line entries.
		name = strings.ReplaceAll(name, "\n", " ")
		name = termformat.Sanitize(name, 4)

		desc := strings.TrimSpace(s.Description)
		desc = strings.ReplaceAll(desc, "\n", " ")
		desc = termformat.Sanitize(desc, 4)

		if wrote > 0 {
			b.WriteByte('\n')
		}

		b.WriteString(primary.Wrap("• " + name))
		if desc != "" {
			b.WriteString(primary.Wrap(" - "))
			b.WriteString(accent.Wrap(desc))
		}
		wrote++
	}

	// If every skill was blank (shouldn't happen), fall back to <none>.
	if wrote == 0 {
		return primary.Wrap("Installed skills:\n• <none>")
	}

	issues = strings.TrimSpace(issues)
	if issues != "" {
		b.WriteString("\n\n")
		b.WriteString(primary.Wrap("Skills with errors:"))
		b.WriteByte('\n')

		for _, line := range strings.Split(issues, "\n") {
			line = strings.TrimRight(line, " \t")
			if strings.TrimSpace(line) == "" {
				continue
			}
			b.WriteString(accent.Wrap(termformat.Sanitize(line, 4)))
			b.WriteByte('\n')
		}
	}

	return b.String()
}

// renderUserMessageBlock returns a fully formated message with proper width and bg color.
func (m *model) renderUserMessageBlock(content string, queued bool, width int) string {
	prompt := "› "
	if m != nil && m.textarea != nil && m.textarea.Prompt != "" {
		prompt = m.textarea.Prompt
	}

	// The user message area should visually match the TextArea:
	// - prompt on the first display line
	// - subsequent display lines (including soft-wrapped lines) align to the first typed column.
	innerWidth := max(width-2, 1) // 2: margin left/right from the BlockStyle below
	sanitized := termformat.Sanitize(content, 4)
	logicalLines := strings.Split(sanitized, "\n")
	if queued && len(logicalLines) > 0 {
		logicalLines[0] = fmt.Sprintf("%s (queued)", logicalLines[0])
	}
	sanitized = strings.Join(logicalLines, "\n")

	lines := tuicontrols.WrapPromptedText(prompt, innerWidth, sanitized)

	content = termformat.Style{Foreground: m.palette.primaryForeground}.Wrap(strings.Join(lines, "\n"))
	bs := termformat.BlockStyle{TotalWidth: width, TextBackground: m.palette.accentBackground, MarginLeft: 1, MarginRight: 1, MarginBackground: m.palette.primaryBackground}

	return bs.Apply(content)
}

// renderWorkingIndicator renders the running-agent status line at width cells.
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

// renderContextStatusLine renders a package-context status line with a status-colored bullet.
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

// workingIndicatorText returns the running-agent status text for the working indicator.
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

// padViewportContentHeight ensures that content, which is the proposed contents of the viewport, has enough height, by adding rows of spaces with a bg color. This
// is nececessary so that the whole message area has the same bg color to the user, even if there's not much actual content in it.
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

// blankRow returns a row of width space cells styled with background. If width is less than one, it returns one cell.
func (m *model) blankRow(width int, background termformat.Color) string {
	if width <= 0 {
		width = 1
	}
	return termformat.Style{Background: background}.Wrap(strings.Repeat(" ", width))
}

// updateTextareaHeight resizes the input area to fit its visible contents within the configured limits.
func (m *model) updateTextareaHeight() {
	lines := 1
	if m.textarea != nil {
		// Use display lines (wrap-aware) so the text area grows based on what the
		// user actually sees, not just logical '\n' lines.
		if m.textarea.Width() > 0 {
			lines = m.textarea.DisplayLines()
			if lines < 1 {
				lines = 1
			}
		} else {
			contents := m.textarea.Contents()
			lines = strings.Count(contents, "\n") + 1
		}
	}
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

// infoLineView renders the bottom help line for the current viewport width.
func (m *model) infoLineView() string {
	hints := []string{"ctrl-c to quit", "esc to clear / stop", "ctrl-j for newline", "ctrl-o overlay"}
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

// infoPanelBlock renders the right-side Info Panel at the configured width and window height.
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

// infoPanelContent returns the Info Panel body assembled from all non-empty sections.
func (m *model) infoPanelContent() string {
	var sections []string
	if versionSection := m.versionUpgradeNoticeSection(); versionSection != "" {
		sections = append(sections, versionSection)
	}
	if usageSection := m.tokensCostSection(); usageSection != "" {
		sections = append(sections, usageSection)
	}
	if pkgSection := m.packageSection(); pkgSection != "" {
		sections = append(sections, pkgSection)
	}
	return strings.Join(sections, "\n\n")
}

// tokensCostSection renders the Info Panel session, model, context, cost, and token summary.
func (m *model) tokensCostSection() string {
	sessionID := "<none>"
	modelID := defaultModelID
	if m != nil && m.session != nil {
		if id := strings.TrimSpace(m.session.ID()); id != "" {
			sessionID = id
		}
	}
	if m != nil {
		modelID = m.currentModelID()
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
		termformat.Sanitize(fmt.Sprintf("Model: %s", formatModelIDWithAuthMarkers(modelID)), 4),
	)
	lines = append(lines, tokensCostLines(info, usage, contextPercent)...)

	return strings.Join(lines, "\n")
}

// packageSection renders the Info Panel package-mode status section.
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

	lines := []string{fmt.Sprintf("Package: %s", pkgPath)}
	if m.shouldShowSpecConformance() {
		lines = append(lines, fmt.Sprintf("• SPEC.md conformance: %s", m.specConformanceIndicator()))
	}
	return strings.Join(lines, "\n")
}

// currentModelInfo returns metadata for the model selected for the active or next session.
func (m *model) currentModelInfo() llmmodel.ModelInfo {
	return llmmodel.GetModelInfo(m.currentModelID())
}

// currentModelID returns the model selected for the active or next session. It falls back to the default model when no session or configured model is available.
func (m *model) currentModelID() llmmodel.ModelID {
	if m == nil {
		return defaultModelID
	}
	if m.session != nil && m.session.modelID != "" {
		return m.session.modelID
	}
	if m.sessionConfig.modelID != "" {
		return m.sessionConfig.modelID
	}
	return defaultModelID
}

func formatModelIDWithAuthMarkers(id llmmodel.ModelID) string {
	name := string(id)
	if strings.TrimSpace(name) == "" {
		name = string(defaultModelID)
		id = defaultModelID
	}
	if llmmodel.ModelUsesProviderSubscription(id) {
		return name + " (" + providerSubscriptionModelMarker + ")"
	}
	return name
}

// currentAgent returns the agent for the active session, or nil when no session is active.
func (m *model) currentAgent() *agent.Agent {
	if m == nil || m.session == nil {
		return nil
	}
	return m.session.agent
}

// startLatestVersionCheck starts the latest-version check if it has not already been started.
func (m *model) startLatestVersionCheck() {
	if m == nil || m.versionCheckStarted || m.monitor == nil || m.tui == nil {
		return
	}
	m.versionCheckStarted = true

	mon := m.monitor
	prog := m.tui
	go func() {
		latest, err := mon.LatestVersionSync()
		prog.Send(latestVersionMsg{latest: latest, err: err})
	}()
}

// permissionView returns the rendered Permission Area, or an empty string when no prompt is visible.
func (m *model) permissionView() string {
	return m.permissionViewText
}

// refreshPermissionView rebuilds the Permission Area from the active permission and current layout.
func (m *model) refreshPermissionView() {
	if m.activePermission == nil {
		m.permissionViewText = ""
		m.permissionViewHeight = 0
		m.updateSizes()
		return
	}

	req := m.activePermission.request

	width := m.viewportWidth
	if width <= 0 {
		width = m.windowWidth
	}
	if width <= 0 {
		width = agentformatter.MinTerminalWidth
	}
	width = max(width, 1)

	// Guard against very narrow terminals; BlockStyle panics if the requested width
	// cannot contain margin/padding/border.
	const (
		permissionMarginLR = 1
		permissionPadding  = 1
		permissionBorderLR = 2 // left+right border columns
	)
	minTotalWidth := 2*permissionMarginLR + 2*permissionPadding + permissionBorderLR
	if width < minTotalWidth {
		rendered := termformat.Sanitize(req.Prompt, 4)
		m.permissionViewText = rendered
		m.permissionViewHeight = strings.Count(rendered, "\n") + 1
		m.updateSizes()
		return
	}

	var b strings.Builder
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		prompt = "Allow this request?"
	}
	prompt = termformat.Sanitize(prompt, 4)
	b.WriteString(termformat.Style{Foreground: m.palette.primaryForeground}.Wrap(prompt))
	b.WriteString("\n\n")
	key := func(s string) string {
		return termformat.Style{Foreground: m.palette.accentForeground}.Wrap(s)
	}
	body := func(s string) string {
		return termformat.Style{Foreground: m.palette.primaryForeground}.Wrap(s)
	}
	b.WriteString(key("Y") + body("    allow\n"))
	b.WriteString(key("N") + body("    deny\n"))
	b.WriteString(key("ESC") + body("  deny + stop agent"))

	rendered := termformat.BlockStyle{
		MarginLeft:         permissionMarginLR,
		MarginRight:        permissionMarginLR,
		Padding:            permissionPadding,
		BorderStyle:        termformat.BorderStyleBasic,
		TotalWidth:         width,
		TextBackground:     m.palette.accentBackground,
		MarginBackground:   m.palette.primaryBackground,
		PaddingBackground:  m.palette.accentBackground,
		BorderForeground:   m.palette.borderColor,
		BorderBackground:   m.palette.primaryBackground,
		BlockNormalizeMode: termformat.BlockNormalizeModeExtend,
	}.Apply(b.String())

	m.permissionViewText = rendered
	m.permissionViewHeight = strings.Count(rendered, "\n") + 1
	m.updateSizes()
}

// isAgentRunning reports whether an agent execution is currently active.
func (m *model) isAgentRunning() bool {
	return m.currentRun != nil
}

// packageContextPending reports whether Package Mode context gathering is still in progress.
func (m *model) packageContextPending() bool {
	return m.packageContext != nil && m.packageContext.status == packageContextStatusPending
}

// updatePlaceholder updates the text area placeholder to reflect whether the agent is running.
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

// enqueuePermissionRequest shows req as the active permission prompt or queues it behind the current prompt.
func (m *model) enqueuePermissionRequest(req authdomain.UserRequest) {
	prompt := &permissionPrompt{request: req}
	if m.activePermission == nil {
		m.activePermission = prompt
		m.refreshPermissionView()
		return
	}
	m.permissionQueue = append(m.permissionQueue, prompt)
}

// triggerPermissionDemo enqueues a synthetic permission request for exercising permission handling.
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

// advancePermissionQueue shows the next queued permission prompt, or hides the Permission Area when none remain.
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

// rejectOutstandingPermissions denies and clears all active and queued permission requests.
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

// startPackageContextGather starts asynchronous initial-context gathering for the active Package Mode session. It records a pending status message and sends the
// completed result back through the TUI runtime.
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
	lintSteps := m.session.config.lintSteps
	go func() {
		contextText, err := buildPackageInitialContext(sandboxDir, pkgPath, pkgAbsPath, lintSteps)
		status := packageContextStatusSuccess
		errMsg := ""
		if err != nil {
			status = packageContextStatusFailure
			errMsg = err.Error()
		}
		m.tui.Send(packageContextResultMsg{
			runID:  runID,
			status: status,
			text:   contextText,
			errMsg: errMsg,
		})
	}()
}

// resetSessionWithConfig replaces the active session with a new session built from cfg and resets session-scoped UI state. On startup failure, it keeps the current
// session and displays the failure in the Messages Area. Callers should use requestSessionResetWithFollowUp while an agent run is still draining.
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
	m.queuedMessages = nil
	m.permissionQueue = nil
	m.activePermission = nil
	m.pendingSessionConfig = nil
	m.pendingPostResetMessage = ""
	m.pendingPostResetUserMessage = ""
	m.pendingPostResetStartRun = false
	m.packageContext = nil
	m.specConformance = nil
	m.resetToolDisplayState()
	m.exitEditingState()
	m.editedHistoryDrafts = nil
	if m.textarea != nil {
		m.textarea.SetContents("")
	}
	m.updateTextareaHeight()
	m.refreshPermissionView()
	m.updatePlaceholder()
	m.startPackageContextGather()
	m.startSpecConformanceCheck()
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

// formatStopwatchDuration returns a human-readable stopwatch string with hour, minute, and second units, clamping negative durations to zero and always showing
// seconds.
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
