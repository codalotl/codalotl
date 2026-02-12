package tui

import (
	"strings"
	"testing"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/q/termformat"
	qtui "github.com/codalotl/codalotl/internal/q/tui"
	"github.com/codalotl/codalotl/internal/skills"
	"github.com/codalotl/codalotl/internal/tools/authdomain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type noopFormatter struct{}

func (noopFormatter) FormatEvent(agent.Event, int) string {
	return ""
}

type stubAuthorizer struct {
	closed bool
}

var _ authdomain.Authorizer = (*stubAuthorizer)(nil)

func (a *stubAuthorizer) SandboxDir() string { return "" }
func (a *stubAuthorizer) CodeUnitDir() string {
	return ""
}
func (a *stubAuthorizer) IsCodeUnitDomain() bool { return false }
func (a *stubAuthorizer) WithoutCodeUnit() authdomain.Authorizer {
	return a
}
func (a *stubAuthorizer) IsAuthorizedForRead(bool, string, string, ...string) error  { return nil }
func (a *stubAuthorizer) IsAuthorizedForWrite(bool, string, string, ...string) error { return nil }
func (a *stubAuthorizer) IsShellAuthorized(bool, string, string, []string) error     { return nil }
func (a *stubAuthorizer) Close()                                                     { a.closed = true }

func TestModelViewAfterResize(t *testing.T) {
	palette := colorPalette{
		colorized:         true,
		primaryBackground: termformat.ANSIColor(1),
		accentBackground:  termformat.ANSIColor(2),
		primaryForeground: termformat.ANSIColor(3),
		accentForeground:  termformat.ANSIColor(4),
	}
	palette.workingSeq = workingIndicatorSequences(palette)

	m := newModel(palette, noopFormatter{}, &session{config: sessionConfig{}}, sessionConfig{}, nil, nil, nil)

	require.False(t, m.ready)
	require.Equal(t, "initializing", m.View())

	const width = 100
	const height = 40
	m.Update(nil, qtui.ResizeEvent{Width: width, Height: height})

	assert.Equal(t, height, m.windowHeight)
	assert.Equal(t, width, m.windowWidth)
	assert.Equal(t, 60, m.viewportWidth)
	assert.Equal(t, 40, m.infoPanelWidth)
	assert.Equal(t, 4, m.textAreaHeight)
	assert.Equal(t, 35, m.viewportHeight)
	assert.Equal(t, 0, m.permissionViewHeight)
	assert.Equal(t, 60, m.viewport.Width())
	assert.Equal(t, 35, m.viewport.Height())

	view := m.View()
	rectWidth := termformat.BlockWidth(view)
	rectHeight := termformat.BlockHeight(view)
	require.Equal(t, width, rectWidth)
	require.Equal(t, height, rectHeight)

	lines := strings.Split(view, "\n")
	require.Equal(t, height, len(lines))
	for i, line := range lines {
		require.Equalf(t, width, termformat.TextWidthWithANSICodes(line), "line %d width mismatch", i)
	}

	requireColorEqual(t, palette.primaryBackground, colorAt(view, 0, 0, true))
	requireColorEqual(t, palette.primaryBackground, colorAt(view, m.viewportWidth-1, m.viewportHeight-1, true))
	requireColorEqual(t, palette.accentBackground, colorAt(view, width-2, 1, true))
	textAreaTop := height - m.textAreaHeight - m.infoLineHeight
	requireColorEqual(t, palette.accentBackground, colorAt(view, 1, textAreaTop+1, true))

	viewNoANSI := stripAnsi(view)

	require.NotEqual(t, "initializing", viewNoANSI)
	require.Contains(t, viewNoANSI, "describing a task")
	require.Contains(t, viewNoANSI, "Ready")
}

func TestFakeAgentEventsCoverage(t *testing.T) {
	events := fakeAgentEvents()
	require.Greater(t, len(events), 0)

	require.Equal(t, agent.EventTypeAssistantReasoning, events[0].Type)
	require.GreaterOrEqual(t, len(events), 2)
	require.Equal(t, agent.EventTypeAssistantText, events[len(events)-2].Type)
	require.Equal(t, agent.EventTypeDoneSuccess, events[len(events)-1].Type)

	planUpdates := 0
	var sawPatch, sawRead, sawList bool
	for _, ev := range events {
		if ev.Type != agent.EventTypeToolComplete || ev.ToolCall == nil {
			continue
		}
		switch ev.ToolCall.Name {
		case "update_plan":
			planUpdates++
		case "apply_patch":
			sawPatch = true
		case "read_file":
			sawRead = true
		case "ls":
			sawList = true
		}
	}

	require.Equal(t, 3, planUpdates)
	require.True(t, sawPatch)
	require.True(t, sawRead)
	require.True(t, sawList)
}

func TestRenderUserMessageBlock_WrappedLinesAlignAfterPrompt(t *testing.T) {
	m := newModel(colorPalette{}, noopFormatter{}, nil, sessionConfig{}, nil, nil, nil)

	const width = 16
	content := strings.Repeat("x", 50) // no spaces => forces wrapping even without word boundaries

	block := m.renderUserMessageBlock(content, false, width)
	view := stripAnsi(block)
	lines := strings.Split(view, "\n")
	require.Greater(t, len(lines), 1)

	for _, line := range lines {
		require.Equal(t, width, termformat.TextWidthWithANSICodes(line))
	}

	require.True(t, strings.HasPrefix(lines[0], " › "))
	for i := 1; i < len(lines); i++ {
		require.True(t, strings.HasPrefix(lines[i], "   "))
	}
}

func TestRenderUserMessageBlock_LogicalNewlinesUseContinuationIndent(t *testing.T) {
	m := newModel(colorPalette{}, noopFormatter{}, nil, sessionConfig{}, nil, nil, nil)

	const width = 40
	block := m.renderUserMessageBlock("first\nsecond", false, width)
	view := stripAnsi(block)
	lines := strings.Split(view, "\n")
	require.Len(t, lines, 2)

	require.True(t, strings.HasPrefix(lines[0], " › "))
	require.True(t, strings.HasPrefix(lines[1], "   "))
}

func TestPermissionCommandTriggersView(t *testing.T) {
	palette := colorPalette{
		primaryBackground: termformat.ANSIColor(0),
		accentBackground:  termformat.ANSIColor(1),
		primaryForeground: termformat.ANSIColor(2),
		accentForeground:  termformat.ANSIColor(3),
	}
	m := newModel(palette, noopFormatter{}, nil, sessionConfig{}, nil, nil, nil)
	m.windowWidth = 80
	m.windowHeight = 20
	m.updateSizes()

	handled := m.handleSlashCommand("/permission")
	require.True(t, handled)
	require.NotNil(t, m.activePermission)

	view := stripAnsi(m.permissionView())
	normalized := strings.Join(strings.Fields(view), " ")
	require.Contains(t, normalized, "demo permission request")

	m.resolvePermission(true)

	require.Nil(t, m.activePermission)
	require.Equal(t, 1, len(m.messages))
	require.Equal(t, messageKindSystem, m.messages[0].kind)
	require.Equal(t, "Demo permission granted.", m.messages[0].userMessage)
}

func TestCyclingModeNavigation(t *testing.T) {
	palette := colorPalette{}
	m := newModel(palette, noopFormatter{}, nil, sessionConfig{}, nil, nil, nil)
	m.windowWidth = 80
	m.windowHeight = 20
	m.updateSizes()

	m.recordSubmittedMessage("first message")
	m.recordSubmittedMessage("second message")
	m.textarea.SetContents("")

	m.Update(nil, qtui.KeyEvent{ControlKey: qtui.ControlKeyUp})
	require.True(t, m.cyclingMode)
	assert.Equal(t, "second message", m.textarea.Contents())

	m.Update(nil, qtui.KeyEvent{ControlKey: qtui.ControlKeyUp})
	require.True(t, m.cyclingMode)
	assert.Equal(t, "first message", m.textarea.Contents())

	m.Update(nil, qtui.KeyEvent{ControlKey: qtui.ControlKeyDown})
	require.True(t, m.cyclingMode)
	assert.Equal(t, "second message", m.textarea.Contents())

	m.Update(nil, qtui.KeyEvent{ControlKey: qtui.ControlKeyDown})
	assert.False(t, m.cyclingMode)
	assert.Equal(t, "", m.textarea.Contents())
}

func TestCyclingModeEditsExitAndReturn(t *testing.T) {
	palette := colorPalette{}
	m := newModel(palette, noopFormatter{}, nil, sessionConfig{}, nil, nil, nil)
	m.windowWidth = 80
	m.windowHeight = 20
	m.updateSizes()

	m.recordSubmittedMessage("first message")
	m.recordSubmittedMessage("second message")
	m.textarea.SetContents("")

	m.Update(nil, qtui.KeyEvent{ControlKey: qtui.ControlKeyUp})
	require.True(t, m.cyclingMode)

	m.Update(nil, qtui.KeyEvent{ControlKey: qtui.ControlKeyNone, Runes: []rune{'!'}})
	require.False(t, m.cyclingMode)
	require.True(t, m.isEditingHistory())

	editedValue := m.textarea.Contents()
	require.Equal(t, "!second message", editedValue)
	require.Equal(t, editedValue, m.historyValue(1))

	m.Update(nil, qtui.KeyEvent{ControlKey: qtui.ControlKeyEsc})
	require.False(t, m.cyclingMode)
	require.False(t, m.isEditingHistory())
	assert.Equal(t, "", m.textarea.Contents())
}

func TestCyclingHistoryFiltersSlashCommands(t *testing.T) {
	m := newModel(colorPalette{}, noopFormatter{}, nil, sessionConfig{}, nil, nil, nil)

	m.recordSubmittedMessage("/new")
	m.recordSubmittedMessage("/model gemini-2.5")
	m.recordSubmittedMessage("/models")
	m.recordSubmittedMessage("/skills")
	m.recordSubmittedMessage("/models gemini-2.5")
	m.recordSubmittedMessage("/refactor fix it")
	m.recordSubmittedMessage("regular input")

	require.Equal(t, []string{"/refactor fix it", "regular input"}, m.messageHistory)
}

func TestPackageCommandStartsSession(t *testing.T) {
	palette := colorPalette{}
	var factoryCfg sessionConfig
	factory := func(cfg sessionConfig) (*session, error) {
		factoryCfg = cfg
		return &session{config: cfg, packagePath: cfg.packagePath}, nil
	}

	m := newModel(palette, noopFormatter{}, nil, sessionConfig{}, factory, nil, nil)
	m.handlePackageCommand(".")

	require.Equal(t, ".", factoryCfg.packagePath)
	require.NotNil(t, m.session)
	assert.Equal(t, ".", m.sessionConfig.packagePath)

	require.Len(t, m.messages, 2)
	assert.Equal(t, messageKindWelcome, m.messages[0].kind)
	if assert.NotNil(t, m.messages[1].contextStatus) {
		assert.Contains(t, m.messages[1].contextStatus.text, "Gathering context for .")
	}
	assert.Equal(t, "Package: .", m.packageSection())
}

func TestPackageCommandRejectsInvalidPath(t *testing.T) {
	m := newModel(colorPalette{}, noopFormatter{}, nil, sessionConfig{}, nil, nil, nil)

	m.handlePackageCommand("no/such/package/path")

	require.Len(t, m.messages, 1)
	assert.Contains(t, m.messages[0].userMessage, "package path")
	assert.Nil(t, m.pendingSessionConfig)
}

func TestPackageSectionFallback(t *testing.T) {
	m := newModel(colorPalette{}, noopFormatter{}, nil, sessionConfig{}, nil, nil, nil)

	section := m.packageSection()
	assert.Contains(t, section, "<none>")
	assert.Contains(t, section, "/package")

	m.sessionConfig = sessionConfig{packagePath: "foo/bar"}
	m.session = &session{packagePath: "foo/bar", config: sessionConfig{packagePath: "foo/bar"}}

	assert.Equal(t, "Package: foo/bar", m.packageSection())
}

func TestCtrlCStopsAgentWhenRunning(t *testing.T) {
	m := newModel(colorPalette{}, noopFormatter{}, nil, sessionConfig{}, nil, nil, nil)

	cancelCalled := false
	m.currentRun = &agentRun{
		cancel: func() { cancelCalled = true },
		events: nil,
		id:     1,
	}

	// If there are queued messages, stopping the agent should restore them to the input.
	m.messageQueue = []string{"one", "two"}
	m.textarea.SetContents("")

	requestCancelCalled := false
	m.requestCancel = func() { requestCancelCalled = true }
	auth := &stubAuthorizer{}
	m.session = &session{authorizer: auth}

	handled := m.handleKeyEvent(qtui.KeyEvent{ControlKey: qtui.ControlKeyCtrlC})
	require.True(t, handled)
	require.True(t, cancelCalled)

	// Should not quit the app when running: do not tear down session/user-request listener here.
	require.False(t, requestCancelCalled)
	require.False(t, auth.closed)

	require.Equal(t, "", strings.Join(m.messageQueue, ",")) // messageQueue cleared
	require.Equal(t, "one\ntwo", m.textarea.Contents())
}

func TestCtrlCQuitsWhenIdle(t *testing.T) {
	m := newModel(colorPalette{}, noopFormatter{}, nil, sessionConfig{}, nil, nil, nil)

	// No currentRun => idle.
	require.False(t, m.isAgentRunning())

	requestCancelCalled := false
	m.requestCancel = func() { requestCancelCalled = true }
	auth := &stubAuthorizer{}
	m.session = &session{authorizer: auth}

	handled := m.handleKeyEvent(qtui.KeyEvent{ControlKey: qtui.ControlKeyCtrlC})
	require.True(t, handled)

	require.True(t, requestCancelCalled)
	require.Nil(t, m.requestCancel)
	require.True(t, auth.closed)
}

func TestEscClearsTextAreaWhenNonEmpty(t *testing.T) {
	m := newModel(colorPalette{}, noopFormatter{}, nil, sessionConfig{}, nil, nil, nil)

	m.textarea.SetContents("hello")
	m.Update(nil, qtui.KeyEvent{ControlKey: qtui.ControlKeyEsc})

	require.Equal(t, "", m.textarea.Contents())
	require.False(t, m.cyclingMode)
	require.False(t, m.isEditingHistory())
}

func TestEscDoesNotStopAgentWhenTextAreaNonEmpty(t *testing.T) {
	m := newModel(colorPalette{}, noopFormatter{}, nil, sessionConfig{}, nil, nil, nil)

	cancelCalled := false
	m.currentRun = &agentRun{
		cancel: func() { cancelCalled = true },
		events: nil,
		id:     1,
	}

	m.textarea.SetContents("typed while running")
	handled := m.handleKeyEvent(qtui.KeyEvent{ControlKey: qtui.ControlKeyEsc})
	require.True(t, handled)
	require.False(t, cancelCalled)
	require.Equal(t, "", m.textarea.Contents())
}

func TestEscStopsAgentWhenTextAreaEmpty(t *testing.T) {
	m := newModel(colorPalette{}, noopFormatter{}, nil, sessionConfig{}, nil, nil, nil)

	cancelCalled := false
	m.currentRun = &agentRun{
		cancel: func() { cancelCalled = true },
		events: nil,
		id:     1,
	}

	m.textarea.SetContents("")
	handled := m.handleKeyEvent(qtui.KeyEvent{ControlKey: qtui.ControlKeyEsc})
	require.True(t, handled)
	require.True(t, cancelCalled)
}

func TestToolResultReplacesCallByDefault(t *testing.T) {
	m := newModel(colorPalette{}, noopFormatter{}, nil, sessionConfig{}, nil, nil, nil)

	callID := "call-1"
	call := &llmstream.ToolCall{CallID: callID, Name: "read_file"}
	result := &llmstream.ToolResult{CallID: callID, Name: "read_file"}

	m.handleAgentEvent(agent.Event{Type: agent.EventTypeToolCall, Tool: "read_file", ToolCall: call})
	require.Len(t, m.messages, 1)
	require.Equal(t, agent.EventTypeToolCall, m.messages[0].event.Type)

	m.handleAgentEvent(agent.Event{Type: agent.EventTypeToolComplete, Tool: "read_file", ToolCall: call, ToolResult: result})

	// Default behavior: the tool call entry is replaced by the result entry.
	require.Len(t, m.messages, 1)
	require.Equal(t, agent.EventTypeToolComplete, m.messages[0].event.Type)
	require.NotNil(t, m.messages[0].event.ToolResult)
	require.Equal(t, callID, m.messages[0].event.ToolResult.CallID)
}

func TestSubAgentToolResultDoesNotReplaceCall(t *testing.T) {
	m := newModel(colorPalette{}, noopFormatter{}, nil, sessionConfig{}, nil, nil, nil)

	callID := "call-2"
	call := &llmstream.ToolCall{CallID: callID, Name: "change_api"}
	result := &llmstream.ToolResult{CallID: callID, Name: "change_api"}

	m.handleAgentEvent(agent.Event{Type: agent.EventTypeToolCall, Tool: "change_api", ToolCall: call})
	require.Len(t, m.messages, 1)
	require.Equal(t, agent.EventTypeToolCall, m.messages[0].event.Type)

	m.handleAgentEvent(agent.Event{Type: agent.EventTypeToolComplete, Tool: "change_api", ToolCall: call, ToolResult: result})

	// Exception behavior: for SubAgent tools, keep the call and append the result.
	require.Len(t, m.messages, 2)
	require.Equal(t, agent.EventTypeToolCall, m.messages[0].event.Type)
	require.Equal(t, agent.EventTypeToolComplete, m.messages[1].event.Type)
}

func TestModelCommandListsAvailableModels(t *testing.T) {
	// Ensure we have at least one usable model to list.
	llmmodel.ConfigureProviderKey(llmmodel.ProviderIDOpenAI, "test-openai-key")
	require.NotEmpty(t, llmmodel.GetAPIKey(llmmodel.DefaultModel))

	m := newModel(colorPalette{}, noopFormatter{}, nil, sessionConfig{}, nil, nil, nil)

	handled := m.handleSlashCommand("/model")
	require.True(t, handled)

	require.Len(t, m.messages, 1)
	msg := m.messages[0]
	require.Equal(t, messageKindSystem, msg.kind)
	assert.Contains(t, msg.userMessage, "Current model:")
	assert.Contains(t, msg.userMessage, "Available models:")
	assert.Contains(t, msg.userMessage, string(llmmodel.DefaultModel))

	// Only list models that have an effective API key.
	for _, id := range listedModelIDs(msg.userMessage) {
		require.Truef(t, id.Valid(), "listed invalid model id: %q", id)
		require.NotEmptyf(t, llmmodel.GetAPIKey(id), "listed model without API key: %q", id)
	}
}

func TestModelsCommandListsAvailableModels(t *testing.T) {
	// Ensure we have at least one usable model to list.
	llmmodel.ConfigureProviderKey(llmmodel.ProviderIDOpenAI, "test-openai-key")
	require.NotEmpty(t, llmmodel.GetAPIKey(llmmodel.DefaultModel))

	m := newModel(colorPalette{}, noopFormatter{}, nil, sessionConfig{}, nil, nil, nil)

	handled := m.handleSlashCommand("/models")
	require.True(t, handled)

	require.Len(t, m.messages, 1)
	msg := m.messages[0]
	require.Equal(t, messageKindSystem, msg.kind)
	assert.Contains(t, msg.userMessage, "Current model:")
	assert.Contains(t, msg.userMessage, "Available models:")
	assert.Contains(t, msg.userMessage, string(llmmodel.DefaultModel))

	// Only list models that have an effective API key.
	for _, id := range listedModelIDs(msg.userMessage) {
		require.Truef(t, id.Valid(), "listed invalid model id: %q", id)
		require.NotEmptyf(t, llmmodel.GetAPIKey(id), "listed model without API key: %q", id)
	}
}

func TestModelsCommandRejectsArgs(t *testing.T) {
	ids := llmmodel.AvailableModelIDs()
	if len(ids) == 0 {
		t.Skip("need at least one available model to test /models")
	}
	target := ids[0]

	var (
		persistCalled bool
		factoryCalled bool
	)
	persist := func(id llmmodel.ModelID) error {
		persistCalled = true
		return nil
	}
	factory := func(cfg sessionConfig) (*session, error) {
		factoryCalled = true
		return &session{config: cfg, modelID: cfg.modelID}, nil
	}

	m := newModel(colorPalette{}, noopFormatter{}, nil, sessionConfig{modelID: llmmodel.DefaultModel}, factory, persist, nil)

	handled := m.handleSlashCommand("/models " + string(target))
	require.True(t, handled)
	require.False(t, factoryCalled)
	require.False(t, persistCalled)

	require.Len(t, m.messages, 1)
	assert.Contains(t, m.messages[0].userMessage, "Usage: `/models`")
	assert.Contains(t, m.messages[0].userMessage, "`/model <id>`")
}

func TestModelCommandSwitchesAndPersistsWhenConfigured(t *testing.T) {
	ids := llmmodel.AvailableModelIDs()
	if len(ids) < 2 {
		t.Skip("need at least two available models to test switching")
	}
	target := ids[0]
	if target == llmmodel.DefaultModel {
		target = ids[1]
	}

	var (
		persistCalled bool
		persistedID   llmmodel.ModelID
		factoryCalled bool
		factoryCfg    sessionConfig
	)
	persist := func(id llmmodel.ModelID) error {
		persistCalled = true
		persistedID = id
		return nil
	}
	factory := func(cfg sessionConfig) (*session, error) {
		factoryCalled = true
		factoryCfg = cfg
		return &session{config: cfg, modelID: cfg.modelID}, nil
	}

	m := newModel(colorPalette{}, noopFormatter{}, nil, sessionConfig{modelID: llmmodel.DefaultModel}, factory, persist, nil)

	handled := m.handleSlashCommand("/model " + string(target))
	require.True(t, handled)
	require.True(t, factoryCalled)
	require.Equal(t, target, factoryCfg.modelID)
	require.True(t, persistCalled)
	require.Equal(t, target, persistedID)

	// Reset creates a new welcome message; /model then appends a confirmation.
	require.GreaterOrEqual(t, len(m.messages), 2)
	assert.Equal(t, messageKindWelcome, m.messages[0].kind)
	assert.Equal(t, messageKindSystem, m.messages[1].kind)
	assert.Contains(t, m.messages[1].userMessage, "Model set to")
	assert.Contains(t, m.messages[1].userMessage, string(target))
}

func listedModelIDs(modelListText string) []llmmodel.ModelID {
	var ids []llmmodel.ModelID
	for _, line := range strings.Split(modelListText, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "• ") {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(line, "• "))
		if rest == "" || strings.HasPrefix(rest, "<none>") {
			continue
		}
		// Lines look like: "<id>" or "<id> (current)".
		if i := strings.IndexAny(rest, " ("); i >= 0 {
			rest = strings.TrimSpace(rest[:i])
		}
		if rest == "" {
			continue
		}
		ids = append(ids, llmmodel.ModelID(rest))
	}
	return ids
}

func TestSkillsCommandListsInstalledSkills(t *testing.T) {
	m := newModel(
		colorPalette{},
		noopFormatter{},
		&session{
			availableSkills: []skills.Skill{
				{Name: "zeta", Description: "last"},
				{Name: "alpha", Description: "first"},
				{Name: "beta", Description: ""},
			},
		},
		sessionConfig{},
		nil,
		nil,
		nil,
	)

	handled := m.handleSlashCommand("/skills")
	require.True(t, handled)

	require.Len(t, m.messages, 2)
	require.Equal(t, messageKindWelcome, m.messages[0].kind)
	require.Equal(t, messageKindSkillsList, m.messages[1].kind)

	m.ensureMessageFormatted(&m.messages[1], 80)
	text := stripAnsi(m.messages[1].formatted)
	require.Contains(t, text, "Installed skills:")

	lines := strings.Split(text, "\n")
	var bullets []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "• ") {
			bullets = append(bullets, line)
		}
	}
	require.Equal(t, []string{
		"• alpha - first",
		"• beta",
		"• zeta - last",
	}, bullets)
}

func TestSkillsCommandRendersNamesNormalAndDescriptionsAccent(t *testing.T) {
	palette := colorPalette{
		colorized:          true,
		primaryBackground:  termformat.ANSIColor(0),
		accentBackground:   termformat.ANSIColor(1),
		primaryForeground:  termformat.ANSIColor(2),
		accentForeground:   termformat.ANSIColor(3),
		colorfulForeground: termformat.ANSIColor(4),
		borderColor:        termformat.ANSIColor(5),
	}
	m := newModel(
		palette,
		noopFormatter{},
		&session{
			availableSkills: []skills.Skill{
				{Name: "alpha", Description: "first"},
			},
		},
		sessionConfig{},
		nil,
		nil,
		nil,
	)

	handled := m.handleSlashCommand("/skills")
	require.True(t, handled)
	require.Len(t, m.messages, 2)
	require.Equal(t, messageKindSkillsList, m.messages[1].kind)

	const width = 80
	m.ensureMessageFormatted(&m.messages[1], width)
	rendered := m.messages[1].formatted

	// Line 1: "• alpha - first"
	//          01234567890
	//          0 2         x positions:
	//          'a' in alpha is at x=2; 'f' in first is at x=10.
	requireColorEqual(t, palette.primaryForeground, colorAt(rendered, 2, 1, false))
	requireColorEqual(t, palette.accentForeground, colorAt(rendered, 10, 1, false))
}

func TestSkillsCommandRejectsArgs(t *testing.T) {
	m := newModel(colorPalette{}, noopFormatter{}, &session{}, sessionConfig{}, nil, nil, nil)

	handled := m.handleSlashCommand("/skills extra")
	require.True(t, handled)

	require.Len(t, m.messages, 2)
	require.Equal(t, messageKindSystem, m.messages[1].kind)
	assert.Contains(t, m.messages[1].userMessage, "Usage: `/skills`")
	assert.NotContains(t, m.messages[1].userMessage, "Installed skills:")
}
