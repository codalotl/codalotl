package noninteractive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/agentbuilder"
	"github.com/codalotl/codalotl/internal/agentformatter"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsPrinted(t *testing.T) {
	t.Parallel()

	base := errors.New("boom")

	if IsPrinted(base) {
		t.Fatalf("IsPrinted(base)=true, want false")
	}

	printed := &printedError{err: base}
	if !IsPrinted(printed) {
		t.Fatalf("IsPrinted(printed)=false, want true")
	}

	wrapped := fmt.Errorf("wrap: %w", printed)
	if !IsPrinted(wrapped) {
		t.Fatalf("IsPrinted(wrapped)=false, want true")
	}
}

func TestShouldTrackTerminalError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ev   agent.Event
		want bool
	}{
		{
			name: "root canceled",
			ev: agent.Event{
				Type:  agent.EventTypeCanceled,
				Agent: agent.AgentMeta{Depth: 0},
			},
			want: true,
		},
		{
			name: "root error",
			ev: agent.Event{
				Type:  agent.EventTypeError,
				Agent: agent.AgentMeta{Depth: 0},
			},
			want: true,
		},
		{
			name: "subagent canceled",
			ev: agent.Event{
				Type:  agent.EventTypeCanceled,
				Agent: agent.AgentMeta{Depth: 1},
			},
			want: false,
		},
		{
			name: "subagent error",
			ev: agent.Event{
				Type:  agent.EventTypeError,
				Agent: agent.AgentMeta{Depth: 2},
			},
			want: false,
		},
		{
			name: "root done success",
			ev: agent.Event{
				Type:  agent.EventTypeDoneSuccess,
				Agent: agent.AgentMeta{Depth: 0},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, shouldTrackTerminalError(tt.ev))
		})
	}
}

func TestExecValidationErrorsPrintNothing(t *testing.T) {
	t.Parallel()

	t.Run("empty prompt", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		if err := Exec("", Options{Out: &buf}); err == nil {
			t.Fatalf("expected error")
		}
		if buf.Len() != 0 {
			t.Fatalf("expected no output, got %q", buf.String())
		}
	})

	t.Run("missing cwd", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		if err := Exec("hello", Options{CWD: "/__definitely_does_not_exist__", Out: &buf}); err == nil {
			t.Fatalf("expected error")
		}
		if buf.Len() != 0 {
			t.Fatalf("expected no output, got %q", buf.String())
		}
	})

	t.Run("package path outside sandbox", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		sandbox := t.TempDir()
		if err := Exec("hello", Options{CWD: sandbox, PackagePath: "..", Out: &buf}); err == nil {
			t.Fatalf("expected error")
		}
		if buf.Len() != 0 {
			t.Fatalf("expected no output, got %q", buf.String())
		}
	})
}

func TestBuildSessionConfig(t *testing.T) {
	t.Parallel()

	t.Run("unsupported slash command", func(t *testing.T) {
		t.Parallel()

		_, err := buildSessionConfig(Options{SlashCommand: "/unknown"})
		require.EqualError(t, err, `unsupported slash command "/unknown"`)
	})

	t.Run("orchestrate ignores package mode and allows empty first prompt", func(t *testing.T) {
		t.Parallel()

		for _, slashCommand := range []string{"orchestrate", "/orchestrate"} {
			config, err := buildSessionConfig(Options{SlashCommand: slashCommand, PackagePath: "internal/noninteractive"})
			require.NoError(t, err)
			require.Equal(t, orchestratorAgentName, config.agentName)
			require.False(t, config.pkgMode)
			require.True(t, config.allowEmptyInitialUser)
		}
	})
}

func TestBuildSessionConfig_ModeSelection(t *testing.T) {
	t.Parallel()

	t.Run("package mode without slash command", func(t *testing.T) {
		t.Parallel()

		config, err := buildSessionConfig(Options{PackagePath: "internal/noninteractive"})
		require.NoError(t, err)
		require.Equal(t, agentbuilder.AgentPackageModeNoContext, config.agentName)
		require.True(t, config.pkgMode)
		require.False(t, config.allowEmptyInitialUser)
	})

	t.Run("orchestrate ignores package mode and uses orchestrator agent", func(t *testing.T) {
		t.Parallel()

		config, err := buildSessionConfig(Options{
			PackagePath:  "internal/noninteractive",
			SlashCommand: "/orchestrate",
		})
		require.NoError(t, err)
		require.Equal(t, orchestratorAgentName, config.agentName)
		require.False(t, config.pkgMode)
		require.True(t, config.allowEmptyInitialUser)
	})
}

func TestBuildAgent_OrchestrateStartBuildsBuiltInOrchestrator(t *testing.T) {
	t.Parallel()

	config, err := buildSessionConfig(Options{
		PackagePath:  "internal/noninteractive",
		SlashCommand: "/orchestrate",
	})
	require.NoError(t, err)

	sandbox := t.TempDir()
	start := sessionStart{
		agentName: config.agentName,
		pkgMode:   config.pkgMode,
	}
	agentInstance, err := buildAgent(start, sandbox, "", "", defaultModelID, authdomain.NewAutoApproveAuthorizer(sandbox), nil)
	require.NoError(t, err)
	require.NotNil(t, agentInstance)
}

func TestShouldSuppressFormattedOutput(t *testing.T) {
	t.Parallel()

	if !shouldSuppressFormattedOutput("• Turn complete: finish=tool_use input=4789 output=100 reasoning=65 cached_input=0") {
		t.Fatalf("expected Turn complete line to be suppressed")
	}
	if shouldSuppressFormattedOutput("• Agent finished the turn.") {
		t.Fatalf("expected finished line not to be suppressed")
	}
	if shouldSuppressFormattedOutput("") {
		t.Fatalf("expected empty string not to be suppressed")
	}
}

func TestFormatAgentFinishedTurnLineIncludesTokens(t *testing.T) {
	t.Parallel()

	line := formatAgentFinishedTurnLine(llmmodel.ModelID("gpt-5.4-high"), llmstream.TokenUsage{
		TotalInputTokens:  10,
		CachedInputTokens: 3,
		TotalOutputTokens: 7,
	})

	want := "• Agent finished the turn. Tokens: input=7 cached_input=3 output=7 total=17"
	if line != want {
		t.Fatalf("got %q, want %q", line, want)
	}
}

func TestFormatAgentFinishedTurnLineIncludesCacheWritesForOpus(t *testing.T) {
	t.Parallel()

	line := formatAgentFinishedTurnLine(llmmodel.ModelID("opus-4.6"), llmstream.TokenUsage{
		TotalInputTokens:         10,
		CachedInputTokens:        3,
		CacheCreationInputTokens: 2,
		TotalOutputTokens:        7,
	})

	want := "• Agent finished the turn. Tokens: input=7 cached_input=3 cache_writes=2 output=7 total=17"
	require.Equal(t, want, line)
}

func TestTurnSnapshotConversation_UsageAndCachingIncludesProviderID(t *testing.T) {
	t.Parallel()

	c := &turnSnapshotConversation{
		turns: []llmstream.Turn{
			{Role: llmstream.RoleSystem},
			{Role: llmstream.RoleUser},
			{
				Role:         llmstream.RoleAssistant,
				ProviderID:   "resp_1",
				FinishReason: llmstream.FinishReasonEndTurn,
				Usage: llmstream.TokenUsage{
					TotalInputTokens:  10,
					CachedInputTokens: 2,
					TotalOutputTokens: 3,
				},
			},
		},
	}

	out := llmstream.UsageAndCaching(c)
	require.Contains(t, out, "resp_1")
}

func TestBuildDoneSuccessReport_IdealCachingPrintsActualAndIdealAndDoesNotAffectUsageAndCaching(t *testing.T) {
	t.Parallel()

	actualTurns := []llmstream.Turn{
		{Role: llmstream.RoleSystem},
		{Role: llmstream.RoleUser},
		{
			Role:         llmstream.RoleAssistant,
			ProviderID:   "resp_actual",
			FinishReason: llmstream.FinishReasonEndTurn,
			Usage: llmstream.TokenUsage{
				TotalInputTokens:  5,
				CachedInputTokens: 1,
				TotalOutputTokens: 2,
			},
		},
	}
	completedAssistantTurnsByAgent := map[string][]llmstream.Turn{
		"agent_main": {
			{
				Role:       llmstream.RoleAssistant,
				ProviderID: "resp_1",
				Usage: llmstream.TokenUsage{
					TotalInputTokens:  10,
					TotalOutputTokens: 1,
				},
			},
			{
				Role:       llmstream.RoleAssistant,
				ProviderID: "resp_2",
				Usage: llmstream.TokenUsage{
					TotalInputTokens:  14,
					TotalOutputTokens: 2,
				},
			},
		},
	}
	actualUsage := llmstream.TokenUsage{
		TotalInputTokens:  100,
		CachedInputTokens: 40,
		TotalOutputTokens: 7,
	}

	withoutIdeal := buildDoneSuccessReport(llmmodel.ModelID("gpt-5.4-high"), actualTurns, completedAssistantTurnsByAgent, actualUsage, false)
	withIdeal := buildDoneSuccessReport(llmmodel.ModelID("gpt-5.4-high"), actualTurns, completedAssistantTurnsByAgent, actualUsage, true)

	require.Contains(t, withoutIdeal.UsageAndCaching, "resp_actual")
	require.Equal(t, withoutIdeal.UsageAndCaching, withIdeal.UsageAndCaching)

	require.Len(t, withoutIdeal.Lines, 1)
	require.Equal(t, "• Agent finished the turn. Tokens: input=60 cached_input=40 output=7 total=107", withoutIdeal.Lines[0])

	require.Len(t, withIdeal.Lines, 2)
	require.Equal(t, "• actual token usage: input=60 cached_input=40 output=7 total=107", withIdeal.Lines[0])
	require.Equal(t, "• Agent finished the turn. Tokens: input=13 cached_input=11 output=3 total=27", withIdeal.Lines[1])
}

func TestBuildDoneSuccessReport_IdealCachingResetsAcrossAgents(t *testing.T) {
	t.Parallel()

	actualTurns := []llmstream.Turn{
		{Role: llmstream.RoleSystem},
		{Role: llmstream.RoleUser},
		{
			Role:         llmstream.RoleAssistant,
			ProviderID:   "resp_actual",
			FinishReason: llmstream.FinishReasonEndTurn,
			Usage: llmstream.TokenUsage{
				TotalInputTokens:  1,
				CachedInputTokens: 0,
				TotalOutputTokens: 1,
			},
		},
	}

	completedAssistantTurnsByAgent := map[string][]llmstream.Turn{
		"agent_1": {
			{
				Role:       llmstream.RoleAssistant,
				ProviderID: "resp_a1_1",
				Usage: llmstream.TokenUsage{
					TotalInputTokens:  10,
					TotalOutputTokens: 1,
				},
			},
			{
				Role:       llmstream.RoleAssistant,
				ProviderID: "resp_a1_2",
				Usage: llmstream.TokenUsage{
					TotalInputTokens:  14,
					TotalOutputTokens: 2,
				},
			},
		},
		"agent_2": {
			{
				Role:       llmstream.RoleAssistant,
				ProviderID: "resp_a2_1",
				Usage: llmstream.TokenUsage{
					TotalInputTokens:  100,
					TotalOutputTokens: 5,
				},
			},
			{
				Role:       llmstream.RoleAssistant,
				ProviderID: "resp_a2_2",
				Usage: llmstream.TokenUsage{
					TotalInputTokens:  90,
					TotalOutputTokens: 7,
				},
			},
		},
	}

	actualUsage := llmstream.TokenUsage{
		TotalInputTokens:  214,
		CachedInputTokens: 0,
		TotalOutputTokens: 15,
	}

	report := buildDoneSuccessReport(llmmodel.ModelID("gpt-5.4-high"), actualTurns, completedAssistantTurnsByAgent, actualUsage, true)
	require.Contains(t, report.UsageAndCaching, "resp_actual")
	require.Len(t, report.Lines, 2)
	require.Equal(t, "• actual token usage: input=214 cached_input=0 output=15 total=229", report.Lines[0])
	require.Equal(t, "• Agent finished the turn. Tokens: input=113 cached_input=101 output=15 total=229", report.Lines[1])
}

func TestBuildDoneSuccessReport_IncludesCacheWritesForOpus(t *testing.T) {
	t.Parallel()

	actualUsage := llmstream.TokenUsage{
		TotalInputTokens:         100,
		CachedInputTokens:        40,
		CacheCreationInputTokens: 9,
		TotalOutputTokens:        7,
	}

	report := buildDoneSuccessReport(llmmodel.ModelID("opus-4.6"), nil, nil, actualUsage, false)
	require.Len(t, report.Lines, 1)
	require.Equal(t, "• Agent finished the turn. Tokens: input=60 cached_input=40 cache_writes=9 output=7 total=107", report.Lines[0])
}

func TestModelReportsCacheWrites(t *testing.T) {
	t.Parallel()

	require.True(t, modelReportsCacheWrites(llmmodel.ModelID("opus-4.6")))
	require.False(t, modelReportsCacheWrites(llmmodel.ModelID("sonnet-4.6")))
	require.False(t, modelReportsCacheWrites(llmmodel.ModelID("gpt-5.4-high")))
}

func TestEffectiveModelID(t *testing.T) {
	t.Parallel()

	t.Run("empty uses default", func(t *testing.T) {
		t.Parallel()
		if got := effectiveModelID(Options{}); got != defaultModelID {
			t.Fatalf("got %q, want %q", got, defaultModelID)
		}
	})

	t.Run("non-empty uses provided", func(t *testing.T) {
		t.Parallel()
		if got := effectiveModelID(Options{ModelID: llmmodel.ModelID("my-model")}); got != "my-model" {
			t.Fatalf("got %q, want %q", got, "my-model")
		}
	})

	t.Run("trims whitespace", func(t *testing.T) {
		t.Parallel()
		if got := effectiveModelID(Options{ModelID: llmmodel.ModelID("  my-model \n")}); got != "my-model" {
			t.Fatalf("got %q, want %q", got, "my-model")
		}
	})
}

func TestDelayedToolCallPrinterFastCompletePrintsOnlyResult(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	out := &lockedWriter{w: &buf}

	p := newDelayedToolCallPrinter(out, 50*time.Millisecond)
	defer p.Close()

	p.Schedule("call_1", "• Read internal/cli/cli.go")
	p.Cancel("call_1") // tool completed quickly
	_, _ = out.Write([]byte("RESULT\n"))

	time.Sleep(200 * time.Millisecond)

	got := buf.String()
	if strings.Contains(got, "• Read internal/cli/cli.go") {
		t.Fatalf("expected tool call line to be suppressed, got %q", got)
	}
	if got != "RESULT\n" {
		t.Fatalf("got %q, want %q", got, "RESULT\n")
	}
}

func TestDelayedToolCallPrinterSlowCompletePrintsCallThenResult(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	out := &lockedWriter{w: &buf}

	delay := 30 * time.Millisecond
	p := newDelayedToolCallPrinter(out, delay)
	defer p.Close()

	p.Schedule("call_2", "• Run tests")

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if strings.Contains(buf.String(), "• Run tests\n") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.Contains(buf.String(), "• Run tests\n") {
		t.Fatalf("expected tool call line to be printed after delay, got %q", buf.String())
	}

	p.Cancel("call_2")
	_, _ = out.Write([]byte("RESULT\n"))

	want := "• Run tests\nRESULT\n"
	if buf.String() != want {
		t.Fatalf("got %q, want %q", buf.String(), want)
	}
}

func TestDelayedToolCallPrinterCloseStopsPendingPrints(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	out := &lockedWriter{w: &buf}

	p := newDelayedToolCallPrinter(out, 40*time.Millisecond)
	p.Schedule("call_3", "• Read something")
	p.Close()

	time.Sleep(200 * time.Millisecond)

	if buf.Len() != 0 {
		t.Fatalf("expected no output after Close, got %q", buf.String())
	}
}

func TestApplyGrantsFromUserPrompt(t *testing.T) {
	t.Parallel()

	t.Run("empty prompt does not call adder", func(t *testing.T) {
		t.Parallel()

		a := authdomain.NewAutoApproveAuthorizer(t.TempDir())
		called := 0
		add := func(_ authdomain.Authorizer, _ string) error {
			called++
			return nil
		}

		if err := applyGrantsFromUserPrompt(a, "   \n\t", add); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if called != 0 {
			t.Fatalf("called=%d, want 0", called)
		}
	})

	t.Run("calls adder with prompt", func(t *testing.T) {
		t.Parallel()

		a := authdomain.NewAutoApproveAuthorizer(t.TempDir())
		called := 0
		var gotAuthorizer authdomain.Authorizer
		var gotPrompt string
		add := func(auth authdomain.Authorizer, msg string) error {
			called++
			gotAuthorizer = auth
			gotPrompt = msg
			return nil
		}

		if err := applyGrantsFromUserPrompt(a, "grant read ./foo", add); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if called != 1 {
			t.Fatalf("called=%d, want 1", called)
		}
		if gotAuthorizer != a {
			t.Fatalf("gotAuthorizer != a")
		}
		if gotPrompt != "grant read ./foo" {
			t.Fatalf("gotPrompt=%q, want %q", gotPrompt, "grant read ./foo")
		}
	})

	t.Run("ErrAuthorizerCannotAcceptGrants is ignored", func(t *testing.T) {
		t.Parallel()

		a := authdomain.NewAutoApproveAuthorizer(t.TempDir())
		add := func(_ authdomain.Authorizer, _ string) error {
			return authdomain.ErrAuthorizerCannotAcceptGrants
		}

		if err := applyGrantsFromUserPrompt(a, "grant read ./foo", add); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
	})

	t.Run("other errors are returned", func(t *testing.T) {
		t.Parallel()

		a := authdomain.NewAutoApproveAuthorizer(t.TempDir())
		want := errors.New("boom")
		add := func(_ authdomain.Authorizer, _ string) error {
			return want
		}

		if err := applyGrantsFromUserPrompt(a, "grant read ./foo", add); !errors.Is(err, want) {
			t.Fatalf("got %v, want %v", err, want)
		}
	})
}

func TestBuildAuthorizerForToolsAppliesGrantsToCodeUnitAuthorizer(t *testing.T) {
	t.Parallel()

	sandbox := t.TempDir()
	pkgRelPath := "internal/mypkg"
	pkgAbsPath := filepath.Join(sandbox, filepath.FromSlash(pkgRelPath))
	if err := os.MkdirAll(pkgAbsPath, 0o755); err != nil {
		t.Fatalf("mkdir pkg: %v", err)
	}

	sandboxAuthorizer := authdomain.NewAutoApproveAuthorizer(sandbox)

	called := 0
	add := func(a authdomain.Authorizer, msg string) error {
		called++
		if !a.IsCodeUnitDomain() {
			t.Fatalf("expected code-unit authorizer, got non-code-unit")
		}
		if strings.TrimSpace(a.CodeUnitDir()) == "" {
			t.Fatalf("expected CodeUnitDir to be non-empty")
		}
		if filepath.Clean(a.CodeUnitDir()) != filepath.Clean(pkgAbsPath) {
			t.Fatalf("CodeUnitDir=%q, want %q", a.CodeUnitDir(), pkgAbsPath)
		}
		if msg != "Read @README.md" {
			t.Fatalf("msg=%q, want %q", msg, "Read @README.md")
		}
		return nil
	}

	a, err := buildAuthorizerForTools(true, pkgRelPath, pkgAbsPath, sandboxAuthorizer, "Read @README.md", add)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a == nil || !a.IsCodeUnitDomain() {
		t.Fatalf("expected code-unit authorizer result")
	}
	if called != 1 {
		t.Fatalf("called=%d, want 1", called)
	}
}

type fakeSessionSend struct {
	events              []agent.Event
	tokenUsage          llmstream.TokenUsage
	contextUsagePercent int
	turns               []llmstream.Turn
}

type fakeSessionAgent struct {
	sends    []fakeSessionSend
	messages []string
	call     int
}

func (a *fakeSessionAgent) SendUserMessage(_ context.Context, message string) <-chan agent.Event {
	a.messages = append(a.messages, message)

	if a.call >= len(a.sends) {
		ch := make(chan agent.Event)
		close(ch)
		return ch
	}

	send := a.sends[a.call]
	ch := make(chan agent.Event, len(send.events))
	a.call++
	for _, ev := range send.events {
		ch <- ev
	}
	close(ch)
	return ch
}

func (a *fakeSessionAgent) TokenUsage() llmstream.TokenUsage {
	if a.call == 0 || a.call > len(a.sends) {
		return llmstream.TokenUsage{}
	}
	return a.sends[a.call-1].tokenUsage
}

func (a *fakeSessionAgent) ContextUsagePercent() int {
	if a.call == 0 || a.call > len(a.sends) {
		return 0
	}
	return a.sends[a.call-1].contextUsagePercent
}

func (a *fakeSessionAgent) Turns() []llmstream.Turn {
	if a.call == 0 || a.call > len(a.sends) {
		return nil
	}
	return a.sends[a.call-1].turns
}

func newTestSession(opts Options, agent sessionAgent, buf *bytes.Buffer) *Session {
	out := io.Writer(buf)
	lockedOut := &lockedWriter{w: out}
	return &Session{
		opts: opts,
		startInfo: stepStartOutput{
			sandboxDir: "/tmp/sandbox",
			modelID:    effectiveModelID(opts),
		},
		out:                            lockedOut,
		jsonWriter:                     newJSONEventWriter(lockedOut),
		formatter:                      agentformatter.NewTUIFormatter(agentformatter.Config{PlainText: true}),
		modelID:                        effectiveModelID(opts),
		agent:                          agent,
		completedAssistantTurnsByAgent: make(map[string][]llmstream.Turn),
	}
}

func textAssistantTurn(text string) *llmstream.Turn {
	return &llmstream.Turn{
		Role: llmstream.RoleAssistant,
		Parts: []llmstream.ContentPart{
			llmstream.TextContent{Content: text},
		},
	}
}

func TestSessionSendUserMessageRequiresPromptOutsideInitialOrchestrateStep(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	session := newTestSession(Options{OutputJSON: true}, &fakeSessionAgent{}, &buf)

	_, err := session.SendUserMessage(context.Background(), "")
	require.EqualError(t, err, "prompt is required")
	require.Empty(t, buf.String())
}

func TestSessionSendUserMessageReusesConversationAcrossSteps(t *testing.T) {
	t.Parallel()

	fake := &fakeSessionAgent{
		sends: []fakeSessionSend{
			{
				events: []agent.Event{
					{
						Type:        agent.EventTypeAssistantText,
						Agent:       agent.AgentMeta{ID: "root", Depth: 0},
						TextContent: llmstream.TextContent{Content: "draft"},
					},
					{
						Type:  agent.EventTypeAssistantTurnComplete,
						Agent: agent.AgentMeta{ID: "root", Depth: 0},
						Turn:  textAssistantTurn("CONTINUE_ITERATION"),
					},
					{
						Type:  agent.EventTypeDoneSuccess,
						Agent: agent.AgentMeta{ID: "root", Depth: 0},
					},
				},
				tokenUsage: llmstream.TokenUsage{
					TotalInputTokens:  10,
					TotalOutputTokens: 2,
				},
				contextUsagePercent: 12,
			},
			{
				events: []agent.Event{
					{
						Type:        agent.EventTypeAssistantText,
						Agent:       agent.AgentMeta{ID: "subagent", Depth: 1},
						TextContent: llmstream.TextContent{Content: "ignore me"},
					},
					{
						Type:  agent.EventTypeAssistantTurnComplete,
						Agent: agent.AgentMeta{ID: "subagent", Depth: 1},
						Turn:  textAssistantTurn("ignore me"),
					},
					{
						Type:        agent.EventTypeAssistantText,
						Agent:       agent.AgentMeta{ID: "root", Depth: 0},
						TextContent: llmstream.TextContent{Content: "STOP_ITERATION"},
					},
					{
						Type:  agent.EventTypeAssistantTurnComplete,
						Agent: agent.AgentMeta{ID: "root", Depth: 0},
						Turn:  textAssistantTurn("STOP_ITERATION"),
					},
					{
						Type:  agent.EventTypeDoneSuccess,
						Agent: agent.AgentMeta{ID: "root", Depth: 0},
					},
				},
				tokenUsage: llmstream.TokenUsage{
					TotalInputTokens:  25,
					CachedInputTokens: 5,
					TotalOutputTokens: 6,
				},
				contextUsagePercent: 34,
			},
		},
	}

	var buf bytes.Buffer
	session := newTestSession(Options{OutputJSON: true}, fake, &buf)

	step1, err := session.SendUserMessage(context.Background(), "step one")
	require.NoError(t, err)
	require.Equal(t, agent.EventTypeDoneSuccess, step1.TerminalEventType)
	require.Equal(t, "CONTINUE_ITERATION", step1.FinalAssistantText)
	require.Equal(t, llmstream.TokenUsage{
		TotalInputTokens:  10,
		TotalOutputTokens: 2,
	}, step1.TokenUsage)
	require.Equal(t, 12, step1.ContextUsagePercent)

	step2, err := session.SendUserMessage(context.Background(), "step two")
	require.NoError(t, err)
	require.Equal(t, agent.EventTypeDoneSuccess, step2.TerminalEventType)
	require.Equal(t, "STOP_ITERATION", step2.FinalAssistantText)
	require.Equal(t, llmstream.TokenUsage{
		TotalInputTokens:  25,
		CachedInputTokens: 5,
		TotalOutputTokens: 6,
	}, step2.TokenUsage)
	require.Equal(t, 34, step2.ContextUsagePercent)
	require.Equal(t, []string{"step one", "step two"}, fake.messages)

	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte{'\n'})
	require.Len(t, lines, 9)

	var firstStart jsonStartEvent
	require.NoError(t, json.Unmarshal(lines[0], &firstStart))
	require.Equal(t, "start", firstStart.Type)

	var firstUser jsonUserMessageEvent
	require.NoError(t, json.Unmarshal(lines[1], &firstUser))
	require.Equal(t, "step one", firstUser.Text)

	var secondStart jsonStartEvent
	require.NoError(t, json.Unmarshal(lines[4], &secondStart))
	require.Equal(t, "start", secondStart.Type)

	var secondUser jsonUserMessageEvent
	require.NoError(t, json.Unmarshal(lines[5], &secondUser))
	require.Equal(t, "step two", secondUser.Text)

	var done jsonDoneEvent
	require.NoError(t, json.Unmarshal(lines[len(lines)-1], &done))
	require.Equal(t, "done", done.Type)
	require.Equal(t, buildJSONTokenUsage(step2.TokenUsage), done.TokenUsage)
}

func TestSessionSendUserMessageReturnsPrintedErrorAndPartialAssistantText(t *testing.T) {
	t.Parallel()

	fake := &fakeSessionAgent{
		sends: []fakeSessionSend{
			{
				events: []agent.Event{
					{
						Type:        agent.EventTypeAssistantText,
						Agent:       agent.AgentMeta{ID: "root", Depth: 0},
						TextContent: llmstream.TextContent{Content: "partial STOP_ITERATION"},
					},
					{
						Type:  agent.EventTypeError,
						Agent: agent.AgentMeta{ID: "root", Depth: 0},
						Error: errors.New("boom"),
					},
				},
				tokenUsage: llmstream.TokenUsage{
					TotalInputTokens:  4,
					TotalOutputTokens: 1,
				},
				contextUsagePercent: 9,
			},
		},
	}

	var buf bytes.Buffer
	session := newTestSession(Options{OutputJSON: true}, fake, &buf)

	step, err := session.SendUserMessage(context.Background(), "continue")
	require.Error(t, err)
	require.True(t, IsPrinted(err))
	require.Equal(t, agent.EventTypeError, step.TerminalEventType)
	require.Equal(t, "partial STOP_ITERATION", step.FinalAssistantText)
	require.Equal(t, llmstream.TokenUsage{
		TotalInputTokens:  4,
		TotalOutputTokens: 1,
	}, step.TokenUsage)
	require.Equal(t, 9, step.ContextUsagePercent)
}

func TestExecUsesSessionAPIAndPreservesTextOutput(t *testing.T) {
	original := newSessionForExec
	t.Cleanup(func() {
		newSessionForExec = original
	})

	fake := &fakeSessionAgent{
		sends: []fakeSessionSend{
			{
				events: []agent.Event{
					{
						Type:  agent.EventTypeAssistantTurnComplete,
						Agent: agent.AgentMeta{ID: "root", Depth: 0},
						Turn:  textAssistantTurn("done"),
					},
					{
						Type:  agent.EventTypeDoneSuccess,
						Agent: agent.AgentMeta{ID: "root", Depth: 0},
					},
				},
				tokenUsage: llmstream.TokenUsage{
					TotalInputTokens:  7,
					TotalOutputTokens: 3,
				},
			},
		},
	}

	var buf bytes.Buffer
	session := newTestSession(Options{NoFormatting: true}, fake, &buf)
	newSessionForExec = func(_ Options) (*Session, error) {
		return session, nil
	}

	err := Exec("fix failing test", Options{NoFormatting: true})
	require.NoError(t, err)
	require.Equal(t, []string{"fix failing test"}, fake.messages)
	require.Contains(t, buf.String(), "> fix failing test\n")
	require.Contains(t, buf.String(), "• Agent finished the turn. Tokens: input=7 cached_input=0 output=3 total=10")
}
