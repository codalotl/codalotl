package tui

import (
	"strings"
	"testing"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/q/termformat"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type noopFormatter struct{}

func (noopFormatter) FormatEvent(agent.Event, int) string {
	return ""
}

func TestModelViewAfterResize(t *testing.T) {
	palette := colorPalette{
		colorized:         true,
		primaryBackground: termformat.ANSIColor(1),
		accentBackground:  termformat.ANSIColor(2),
		primaryForeground: termformat.ANSIColor(3),
		accentForeground:  termformat.ANSIColor(4),
	}
	palette.workingSeq = workingIndicatorSequences(palette)

	m := newModel(palette, noopFormatter{}, &session{config: sessionConfig{}}, sessionConfig{}, nil)

	require.False(t, m.ready)
	require.Equal(t, "initializing", m.View())

	const width = 100
	const height = 40
	_, _ = m.Update(tea.WindowSizeMsg{Width: width, Height: height})

	assert.Equal(t, height, m.windowHeight)
	assert.Equal(t, width, m.windowWidth)
	assert.Equal(t, 60, m.viewportWidth)
	assert.Equal(t, 40, m.infoPanelWidth)
	assert.Equal(t, 4, m.textAreaHeight)
	assert.Equal(t, 35, m.viewportHeight)
	assert.Equal(t, 0, m.permissionViewHeight)
	assert.Equal(t, 60, m.viewport.Width)
	assert.Equal(t, 35, m.viewport.Height)

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

func TestPermissionCommandTriggersView(t *testing.T) {
	palette := colorPalette{
		primaryBackground: termformat.ANSIColor(0),
		accentBackground:  termformat.ANSIColor(1),
		primaryForeground: termformat.ANSIColor(2),
		accentForeground:  termformat.ANSIColor(3),
	}
	m := newModel(palette, noopFormatter{}, nil, sessionConfig{}, nil)
	m.viewportWidth = 80
	m.viewport.Width = 80

	handled, cmd := m.handleSlashCommand("/permission")
	require.True(t, handled)
	require.Nil(t, cmd)
	require.NotNil(t, m.activePermission)

	view := stripAnsi(m.permissionView())
	require.Contains(t, view, "demo permission request")

	m.resolvePermission(true)

	require.Nil(t, m.activePermission)
	require.Equal(t, 1, len(m.messages))
	require.Equal(t, messageKindSystem, m.messages[0].kind)
	require.Equal(t, "Demo permission granted.", m.messages[0].userMessage)
}

func TestCyclingModeNavigation(t *testing.T) {
	palette := colorPalette{}
	m := newModel(palette, noopFormatter{}, nil, sessionConfig{}, nil)
	m.windowWidth = 80
	m.windowHeight = 20
	m.updateSizes()
	m.textarea.Focus()

	m.recordSubmittedMessage("first message")
	m.recordSubmittedMessage("second message")
	m.textarea.Reset()

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	require.True(t, m.cyclingMode)
	assert.Equal(t, "second message", m.textarea.Value())

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	require.True(t, m.cyclingMode)
	assert.Equal(t, "first message", m.textarea.Value())

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	require.True(t, m.cyclingMode)
	assert.Equal(t, "second message", m.textarea.Value())

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	assert.False(t, m.cyclingMode)
	assert.Equal(t, "", m.textarea.Value())
}

func TestCyclingModeEditsExitAndReturn(t *testing.T) {
	palette := colorPalette{}
	m := newModel(palette, noopFormatter{}, nil, sessionConfig{}, nil)
	m.windowWidth = 80
	m.windowHeight = 20
	m.updateSizes()
	m.textarea.Focus()

	m.recordSubmittedMessage("first message")
	m.recordSubmittedMessage("second message")
	m.textarea.Reset()

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	require.True(t, m.cyclingMode)

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'!'}})
	require.False(t, m.cyclingMode)
	require.True(t, m.isEditingHistory())

	editedValue := m.textarea.Value()
	require.Equal(t, "!second message", editedValue)
	require.Equal(t, editedValue, m.historyValue(1))

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	require.True(t, m.cyclingMode)
	require.False(t, m.isEditingHistory())
	assert.Equal(t, editedValue, m.textarea.Value())
}

func TestCyclingHistoryFiltersSlashCommands(t *testing.T) {
	m := newModel(colorPalette{}, noopFormatter{}, nil, sessionConfig{}, nil)

	m.recordSubmittedMessage("/new")
	m.recordSubmittedMessage("/model gemini-2.5")
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

	m := newModel(palette, noopFormatter{}, nil, sessionConfig{}, factory)
	m.handlePackageCommand(".")

	require.Equal(t, ".", factoryCfg.packagePath)
	require.NotNil(t, m.session)
	assert.Equal(t, ".", m.sessionConfig.packagePath)

	require.Len(t, m.messages, 2)
	assert.Contains(t, m.messages[0].userMessage, "Switching to package mode")
	assert.Contains(t, m.messages[1].userMessage, "Session ")
	assert.Equal(t, "Package: .", m.packageSection())
}

func TestPackageCommandRejectsInvalidPath(t *testing.T) {
	m := newModel(colorPalette{}, noopFormatter{}, nil, sessionConfig{}, nil)

	m.handlePackageCommand("no/such/package/path")

	require.Len(t, m.messages, 1)
	assert.Contains(t, m.messages[0].userMessage, "package path")
	assert.Nil(t, m.pendingSessionConfig)
}

func TestPackageSectionFallback(t *testing.T) {
	m := newModel(colorPalette{}, noopFormatter{}, nil, sessionConfig{}, nil)

	section := m.packageSection()
	assert.Contains(t, section, "<none>")
	assert.Contains(t, section, "/package")

	m.sessionConfig = sessionConfig{packagePath: "foo/bar"}
	m.session = &session{packagePath: "foo/bar", config: sessionConfig{packagePath: "foo/bar"}}

	assert.Equal(t, "Package: foo/bar", m.packageSection())
}
