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

func TestBuildAuthorizerForToolsPackageModeUsesDefaultGoCodeUnitScope(t *testing.T) {
	t.Parallel()

	sandbox := t.TempDir()
	pkgRelPath := "internal/mypkg"
	pkgAbsPath := filepath.Join(sandbox, filepath.FromSlash(pkgRelPath))

	require.NoError(t, os.MkdirAll(filepath.Join(pkgAbsPath, "data"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(pkgAbsPath, "fixtures", "testdata"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(pkgAbsPath, ".cache"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(pkgAbsPath, "childpkg"), 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(pkgAbsPath, "mypkg.go"), []byte("package mypkg\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(pkgAbsPath, "data", "config.yml"), []byte("value: 1\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(pkgAbsPath, "fixtures", "testdata", "fixture.go"), []byte("package fixture\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(pkgAbsPath, ".cache", "note.txt"), []byte("hidden\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(pkgAbsPath, "childpkg", "child.go"), []byte("package childpkg\n"), 0o644))

	authorizer, err := buildAuthorizerForTools(
		true,
		pkgRelPath,
		pkgAbsPath,
		authdomain.NewAutoApproveAuthorizer(sandbox),
		"",
		nil,
	)
	require.NoError(t, err)
	t.Cleanup(authorizer.Close)

	assert.NoError(t, authorizer.IsAuthorizedForRead(false, "", "read_file", filepath.Join(pkgAbsPath, "mypkg.go")))
	assert.NoError(t, authorizer.IsAuthorizedForRead(false, "", "read_file", filepath.Join(pkgAbsPath, "data", "config.yml")))
	assert.NoError(t, authorizer.IsAuthorizedForRead(false, "", "read_file", filepath.Join(pkgAbsPath, "fixtures", "testdata", "fixture.go")))

	assert.Error(t, authorizer.IsAuthorizedForRead(false, "", "read_file", filepath.Join(pkgAbsPath, ".cache", "note.txt")))
	assert.Error(t, authorizer.IsAuthorizedForRead(false, "", "read_file", filepath.Join(pkgAbsPath, "childpkg", "child.go")))
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

type namedTestTool struct {
	name string
}

func (t namedTestTool) Info() llmstream.ToolInfo {
	return llmstream.ToolInfo{Name: t.name}
}

func (t namedTestTool) Name() string {
	return t.name
}

func (t namedTestTool) Presenter() llmstream.Presenter {
	return nil
}

func (t namedTestTool) Run(context.Context, llmstream.ToolCall) llmstream.ToolResult {
	return llmstream.ToolResult{}
}

type presenterBackedTestTool struct {
	name string
}

func (t presenterBackedTestTool) Info() llmstream.ToolInfo {
	return llmstream.ToolInfo{Name: t.name}
}

func (t presenterBackedTestTool) Name() string {
	return t.name
}

func (t presenterBackedTestTool) Presenter() llmstream.Presenter {
	return presenterBackedTestPresenter{}
}

func (t presenterBackedTestTool) Run(context.Context, llmstream.ToolCall) llmstream.ToolResult {
	return llmstream.ToolResult{}
}

type presenterBackedTestPresenter struct{}

func (presenterBackedTestPresenter) Present(call llmstream.ToolCall, result *llmstream.ToolResult) llmstream.Presentation {
	summary := "Presenter call " + call.Name
	if result != nil {
		summary = "Presenter done " + result.Name
	}

	return llmstream.Presentation{
		Behavior: llmstream.CompletionBehaviorReplace,
		Summary: llmstream.Line{
			Segments: []llmstream.Segment{
				{Text: summary, Role: llmstream.RoleNormal},
			},
		},
	}
}

type finalMessageBackedTestTool struct {
	name  string
	block llmstream.Block
}

func (t finalMessageBackedTestTool) Info() llmstream.ToolInfo {
	return llmstream.ToolInfo{Name: t.name}
}

func (t finalMessageBackedTestTool) Name() string {
	return t.name
}

func (t finalMessageBackedTestTool) Presenter() llmstream.Presenter {
	return finalMessageBackedTestPresenter{block: t.block}
}

func (t finalMessageBackedTestTool) Run(context.Context, llmstream.ToolCall) llmstream.ToolResult {
	return llmstream.ToolResult{}
}

type finalMessageBackedTestPresenter struct {
	block llmstream.Block
}

func (p finalMessageBackedTestPresenter) Present(call llmstream.ToolCall, result *llmstream.ToolResult) llmstream.Presentation {
	summary := "Presenter call " + call.Name
	if result != nil {
		summary = "Presenter done " + result.Name
	}

	return llmstream.Presentation{
		Behavior: llmstream.CompletionBehaviorReplace,
		Summary: llmstream.Line{
			Segments: []llmstream.Segment{
				{Text: summary, Role: llmstream.RoleNormal},
			},
		},
	}
}

func (p finalMessageBackedTestPresenter) SubagentFinalMessage(llmstream.ToolCall, string, string) llmstream.Block {
	return p.block
}

type recordingFormatter struct {
	events []agent.Event
}

func (f *recordingFormatter) FormatEvent(e agent.Event, _ int) string {
	f.events = append(f.events, e)

	switch e.Type {
	case agent.EventTypeToolCall:
		if e.ToolCall == nil {
			return "CALL"
		}
		return "CALL " + e.ToolCall.Name
	case agent.EventTypeToolComplete:
		if e.ToolResult == nil {
			return "DONE"
		}
		return "DONE " + e.ToolResult.Name
	default:
		return ""
	}
}

type verboseRecordingFormatter struct{}

func (verboseRecordingFormatter) FormatEvent(e agent.Event, _ int) string {
	switch e.Type {
	case agent.EventTypeAssistantText:
		return "TEXT " + e.TextContent.Content
	case agent.EventTypeAssistantReasoning:
		return "REASON " + e.ReasoningContent.Content
	case agent.EventTypeToolCall:
		if e.ToolCall == nil {
			return "CALL"
		}
		return "CALL " + e.ToolCall.Name
	case agent.EventTypeToolComplete:
		if e.ToolResult == nil {
			return "DONE"
		}
		return "DONE " + e.ToolResult.Name
	default:
		return ""
	}
}

type startSubagentRecordingFormatter struct{}

func (startSubagentRecordingFormatter) FormatEvent(e agent.Event, _ int) string {
	switch e.Type {
	case agent.EventTypeStartSubagent:
		return "START SUBAGENT"
	default:
		return ""
	}
}

type countingAuthorizer struct {
	sandboxDir string
	requests   chan authdomain.UserRequest
	closeCount int
	closed     bool
}

func (a *countingAuthorizer) SandboxDir() string {
	if a == nil {
		return ""
	}
	return a.sandboxDir
}

func (a *countingAuthorizer) CodeUnitDir() string { return "" }

func (a *countingAuthorizer) IsCodeUnitDomain() bool { return false }

func (a *countingAuthorizer) WithoutCodeUnit() authdomain.Authorizer { return a }

func (a *countingAuthorizer) IsAuthorizedForRead(bool, string, string, ...string) error { return nil }

func (a *countingAuthorizer) IsAuthorizedForWrite(bool, string, string, ...string) error { return nil }

func (a *countingAuthorizer) IsShellAuthorized(bool, string, string, []string) error { return nil }

func (a *countingAuthorizer) Close() {
	if a == nil {
		return
	}
	a.closeCount++
	if a.closed {
		return
	}
	a.closed = true
	if a.requests != nil {
		close(a.requests)
	}
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

func TestToolNameFromEvent(t *testing.T) {
	t.Parallel()

	tool := namedTestTool{name: "tool_name"}

	require.Equal(t, "tool_name", toolNameFromEvent(agent.Event{
		Tool: &tool,
		ToolCall: &llmstream.ToolCall{
			Name: "call_name",
		},
		ToolResult: &llmstream.ToolResult{
			Name: "result_name",
		},
	}))

	require.Equal(t, "call_name", toolNameFromEvent(agent.Event{
		ToolCall: &llmstream.ToolCall{
			Name: "call_name",
		},
		ToolResult: &llmstream.ToolResult{
			Name: "result_name",
		},
	}))

	require.Equal(t, "result_name", toolNameFromEvent(agent.Event{
		ToolResult: &llmstream.ToolResult{
			Name: "result_name",
		},
	}))

	require.Empty(t, toolNameFromEvent(agent.Event{}))
}

func TestLegacyFormattedToolEventPreservesToolAndBackfillsNames(t *testing.T) {
	t.Parallel()

	tool := namedTestTool{name: "read_file"}

	callEvent := legacyFormattedToolEvent(agent.Event{
		Type: agent.EventTypeToolCall,
		Tool: &tool,
		ToolCall: &llmstream.ToolCall{
			CallID: "call_1",
			Name:   "ignored_call_name",
			Type:   "function_call",
		},
	})
	require.NotNil(t, callEvent.Tool)
	require.Equal(t, "read_file", callEvent.Tool.Name())
	require.NotNil(t, callEvent.ToolCall)
	require.Equal(t, "read_file", callEvent.ToolCall.Name)
	require.Equal(t, "call_1", callEvent.ToolCall.CallID)
	require.Equal(t, "function_call", callEvent.ToolCall.Type)

	completeEvent := legacyFormattedToolEvent(agent.Event{
		Type: agent.EventTypeToolComplete,
		Tool: &tool,
		ToolResult: &llmstream.ToolResult{
			CallID: "call_1",
			Name:   "ignored_result_name",
			Type:   "function_call",
		},
	})
	require.NotNil(t, completeEvent.Tool)
	require.Equal(t, "read_file", completeEvent.Tool.Name())
	require.NotNil(t, completeEvent.ToolCall)
	require.Equal(t, "read_file", completeEvent.ToolCall.Name)
	require.Equal(t, "call_1", completeEvent.ToolCall.CallID)
	require.Equal(t, "function_call", completeEvent.ToolCall.Type)
	require.NotNil(t, completeEvent.ToolResult)
	require.Equal(t, "read_file", completeEvent.ToolResult.Name)
	require.Equal(t, "call_1", completeEvent.ToolResult.CallID)
	require.Equal(t, "function_call", completeEvent.ToolResult.Type)
}

func TestSessionSendUserMessageRequiresPromptOutsideInitialOrchestrateStep(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	session := newTestSession(Options{OutputJSON: true}, &fakeSessionAgent{}, &buf)

	_, err := session.SendUserMessage(context.Background(), "")
	require.EqualError(t, err, "prompt is required")
	require.Empty(t, buf.String())
}

func TestSessionCloseIsIdempotentAndPreventsFurtherUse(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	fake := &fakeSessionAgent{}
	session := newTestSession(Options{OutputJSON: true}, fake, &buf)
	authorizer := &countingAuthorizer{sandboxDir: t.TempDir()}
	session.authorizer = authorizer

	require.NoError(t, session.Close())
	require.NoError(t, session.Close())
	require.Equal(t, 1, authorizer.closeCount)

	_, err := session.SendUserMessage(context.Background(), "after close")
	require.EqualError(t, err, "session is closed")
	require.Empty(t, fake.messages)
}

func TestSessionCloseWaitsForPermissionLoop(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	authorizer := &countingAuthorizer{
		sandboxDir: t.TempDir(),
		requests:   make(chan authdomain.UserRequest),
	}
	session := newTestSession(Options{}, &fakeSessionAgent{}, &buf)
	session.authorizer = authorizer

	exited := make(chan struct{})
	session.requestLoopWG.Add(1)
	go func() {
		defer session.requestLoopWG.Done()
		autoRespondToUserRequests(authorizer.requests, session.out, false, session.jsonWriter, false)
		close(exited)
	}()

	require.NoError(t, session.Close())
	require.Equal(t, 1, authorizer.closeCount)

	select {
	case <-exited:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("permission loop did not exit after Close")
	}
}

func TestSessionSendUserMessageReusesConversationAcrossSteps(t *testing.T) {
	t.Parallel()

	fake := &fakeSessionAgent{
		sends: []fakeSessionSend{
			{
				events: []agent.Event{
					{
						Type:                    agent.EventTypeAssistantText,
						Agent:                   agent.AgentMeta{ID: "root", Depth: 0},
						AssistantTextFinalizing: true,
						TextContent:             llmstream.TextContent{Content: "CONTINUE_ITERATION"},
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
						Type:                    agent.EventTypeAssistantText,
						Agent:                   agent.AgentMeta{ID: "subagent", Depth: 1},
						AssistantTextFinalizing: true,
						TextContent:             llmstream.TextContent{Content: "ignore me"},
					},
					{
						Type:  agent.EventTypeAssistantTurnComplete,
						Agent: agent.AgentMeta{ID: "subagent", Depth: 1},
						Turn:  textAssistantTurn("ignore me"),
					},
					{
						Type:                    agent.EventTypeAssistantText,
						Agent:                   agent.AgentMeta{ID: "root", Depth: 0},
						AssistantTextFinalizing: true,
						TextContent:             llmstream.TextContent{Content: "STOP_ITERATION"},
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

func TestSessionSendUserMessageReturnsPrintedErrorWithoutNonFinalAssistantText(t *testing.T) {
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
	require.Empty(t, step.FinalAssistantText)
	require.Equal(t, llmstream.TokenUsage{
		TotalInputTokens:  4,
		TotalOutputTokens: 1,
	}, step.TokenUsage)
	require.Equal(t, 9, step.ContextUsagePercent)
}

func TestSessionSendUserMessageUsesLegacyToolFormattingWithToolObjectName(t *testing.T) {
	originalDelay := toolCallPrintDelay
	toolCallPrintDelay = 0
	t.Cleanup(func() {
		toolCallPrintDelay = originalDelay
	})

	tool := namedTestTool{name: "read_file"}
	fake := &fakeSessionAgent{
		sends: []fakeSessionSend{
			{
				events: []agent.Event{
					{
						Type:  agent.EventTypeToolCall,
						Agent: agent.AgentMeta{ID: "root", Depth: 0},
						Tool:  &tool,
						ToolCall: &llmstream.ToolCall{
							CallID: "call_1",
							Name:   "ignored_call_name",
						},
					},
					{
						Type:  agent.EventTypeToolComplete,
						Agent: agent.AgentMeta{ID: "root", Depth: 0},
						Tool:  &tool,
						ToolResult: &llmstream.ToolResult{
							CallID: "call_1",
							Name:   "ignored_result_name",
						},
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
	formatter := &recordingFormatter{}
	session.formatter = formatter

	step, err := session.SendUserMessage(context.Background(), "fix failing test")
	require.NoError(t, err)
	require.Equal(t, agent.EventTypeDoneSuccess, step.TerminalEventType)

	require.Contains(t, buf.String(), "CALL read_file\n")
	require.Contains(t, buf.String(), "DONE read_file\n")
	require.NotContains(t, buf.String(), "ignored_call_name")
	require.NotContains(t, buf.String(), "ignored_result_name")

	require.Len(t, formatter.events, 2)
	require.NotNil(t, formatter.events[0].Tool)
	require.Equal(t, "read_file", formatter.events[0].Tool.Name())
	require.Equal(t, "read_file", formatter.events[0].ToolCall.Name)
	require.NotNil(t, formatter.events[1].Tool)
	require.Equal(t, "read_file", formatter.events[1].Tool.Name())
	require.Equal(t, "read_file", formatter.events[1].ToolResult.Name)
}

func TestSessionSendUserMessageUsesPresenterBackedToolFormattingInHumanReadableMode(t *testing.T) {
	originalDelay := toolCallPrintDelay
	toolCallPrintDelay = 0
	t.Cleanup(func() {
		toolCallPrintDelay = originalDelay
	})

	tool := presenterBackedTestTool{name: "read_file"}
	fake := &fakeSessionAgent{
		sends: []fakeSessionSend{
			{
				events: []agent.Event{
					{
						Type:  agent.EventTypeToolCall,
						Agent: agent.AgentMeta{ID: "root", Depth: 0},
						Tool:  tool,
						ToolCall: &llmstream.ToolCall{
							CallID: "call_1",
							Name:   "ignored_call_name",
						},
					},
					{
						Type:  agent.EventTypeToolComplete,
						Agent: agent.AgentMeta{ID: "root", Depth: 0},
						Tool:  tool,
						ToolResult: &llmstream.ToolResult{
							CallID: "call_1",
							Name:   "ignored_result_name",
						},
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

	step, err := session.SendUserMessage(context.Background(), "fix failing test")
	require.NoError(t, err)
	require.Equal(t, agent.EventTypeDoneSuccess, step.TerminalEventType)

	output := buf.String()
	require.Contains(t, output, "Presenter call read_file\n")
	require.Contains(t, output, "Presenter done read_file\n")
	require.NotContains(t, output, "ignored_call_name")
	require.NotContains(t, output, "ignored_result_name")
	require.NotContains(t, output, "Tool read_file")
}

func TestSessionSendUserMessageJSONToolEventsRemainUnchanged(t *testing.T) {
	t.Parallel()

	tool := presenterBackedTestTool{name: "read_file"}
	fake := &fakeSessionAgent{
		sends: []fakeSessionSend{
			{
				events: []agent.Event{
					{
						Type:  agent.EventTypeToolCall,
						Agent: agent.AgentMeta{ID: "root", Depth: 0},
						Tool:  tool,
						ToolCall: &llmstream.ToolCall{
							CallID: "call_1",
							Name:   "ignored_call_name",
							Type:   "function_call",
							Input:  `{"path":"foo.go"}`,
						},
					},
					{
						Type:  agent.EventTypeToolComplete,
						Agent: agent.AgentMeta{ID: "root", Depth: 0},
						Tool:  tool,
						ToolResult: &llmstream.ToolResult{
							CallID:  "call_1",
							Name:    "ignored_result_name",
							Type:    "function_call",
							Result:  "package foo\n",
							IsError: false,
						},
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
	session := newTestSession(Options{OutputJSON: true}, fake, &buf)

	step, err := session.SendUserMessage(context.Background(), "fix failing test")
	require.NoError(t, err)
	require.Equal(t, agent.EventTypeDoneSuccess, step.TerminalEventType)

	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte{'\n'})
	require.Len(t, lines, 5)

	var toolCall map[string]any
	require.NoError(t, json.Unmarshal(lines[2], &toolCall))
	require.Equal(t, map[string]any{
		"type": "tool_call",
		"agent": map[string]any{
			"id":    "root",
			"depth": float64(0),
		},
		"tool": map[string]any{
			"call_id": "call_1",
			"name":    "read_file",
			"type":    "function_call",
			"input":   `{"path":"foo.go"}`,
		},
	}, toolCall)

	var toolComplete map[string]any
	require.NoError(t, json.Unmarshal(lines[3], &toolComplete))
	require.Equal(t, map[string]any{
		"type": "tool_complete",
		"agent": map[string]any{
			"id":    "root",
			"depth": float64(0),
		},
		"tool": map[string]any{
			"call_id": "call_1",
			"name":    "read_file",
			"type":    "function_call",
		},
		"result": map[string]any{
			"output":   "package foo\n",
			"is_error": false,
		},
	}, toolComplete)
}

func TestSessionSendUserMessageDoesNotPrintStartSubagentInHumanReadableOutput(t *testing.T) {
	t.Parallel()

	fake := &fakeSessionAgent{
		sends: []fakeSessionSend{
			{
				events: []agent.Event{
					{
						Type:  agent.EventTypeStartSubagent,
						Agent: agent.AgentMeta{ID: "reviewer", Depth: 1, Parent: "root"},
						StartSubagent: agent.StartSubagent{
							CallingAgentID: "root",
							ToolCallID:     "call_review",
							Label:          "review subagent",
						},
					},
					{
						Type:  agent.EventTypeDoneSuccess,
						Agent: agent.AgentMeta{ID: "root", Depth: 0},
					},
				},
			},
		},
	}

	var buf bytes.Buffer
	session := newTestSession(Options{NoFormatting: true}, fake, &buf)
	session.formatter = startSubagentRecordingFormatter{}

	step, err := session.SendUserMessage(context.Background(), "review this change")
	require.NoError(t, err)
	require.Equal(t, agent.EventTypeDoneSuccess, step.TerminalEventType)
	require.NotContains(t, buf.String(), "START SUBAGENT")
}

func TestSessionSendUserMessageSuppressesDescendantFinalAssistantTextInHumanReadableOutput(t *testing.T) {
	originalDelay := toolCallPrintDelay
	toolCallPrintDelay = 0
	t.Cleanup(func() {
		toolCallPrintDelay = originalDelay
	})

	reviewTool := finalMessageBackedTestTool{name: "review"}
	clarifyTool := namedTestTool{name: "clarify_public_api"}
	childAgent := agent.AgentMeta{ID: "reviewer", Depth: 1, Parent: "root"}
	grandchildAgent := agent.AgentMeta{ID: "clarifier", Depth: 2, Parent: "reviewer"}

	fake := &fakeSessionAgent{
		sends: []fakeSessionSend{
			{
				events: []agent.Event{
					{
						Type:  agent.EventTypeToolCall,
						Agent: agent.AgentMeta{ID: "root", Depth: 0},
						Tool:  reviewTool,
						ToolCall: &llmstream.ToolCall{
							CallID: "call_review",
							Name:   "ignored_review_name",
						},
					},
					{
						Type:        agent.EventTypeAssistantText,
						Agent:       childAgent,
						TextContent: llmstream.TextContent{Content: "looked at files"},
					},
					{
						Type:  agent.EventTypeAssistantTurnComplete,
						Agent: childAgent,
						Turn:  textAssistantTurn("looked at files"),
					},
					{
						Type:  agent.EventTypeToolCall,
						Agent: childAgent,
						Tool:  &clarifyTool,
						ToolCall: &llmstream.ToolCall{
							CallID: "call_clarify",
							Name:   "ignored_clarify_name",
						},
					},
					{
						Type:  agent.EventTypeStartSubagent,
						Agent: grandchildAgent,
						StartSubagent: agent.StartSubagent{
							CallingAgentID: "reviewer",
							ToolCallID:     "call_clarify",
							Label:          "clarify_public_api",
						},
					},
					{
						Type:                    agent.EventTypeAssistantText,
						Agent:                   grandchildAgent,
						AssistantTextFinalizing: true,
						TextContent:             llmstream.TextContent{Content: "checked docs"},
					},
					{
						Type:  agent.EventTypeAssistantTurnComplete,
						Agent: grandchildAgent,
						Turn:  textAssistantTurn("checked docs"),
					},
					{
						Type:  agent.EventTypeDoneSuccess,
						Agent: grandchildAgent,
					},
					{
						Type:  agent.EventTypeToolComplete,
						Agent: childAgent,
						Tool:  &clarifyTool,
						ToolResult: &llmstream.ToolResult{
							CallID: "call_clarify",
							Name:   "ignored_clarify_result",
						},
					},
					{
						Type:                    agent.EventTypeAssistantText,
						Agent:                   childAgent,
						AssistantTextFinalizing: true,
						TextContent:             llmstream.TextContent{Content: `{"decision":"approve"}`},
					},
					{
						Type:  agent.EventTypeAssistantTurnComplete,
						Agent: childAgent,
						Turn:  textAssistantTurn(`{"decision":"approve"}`),
					},
					{
						Type:  agent.EventTypeDoneSuccess,
						Agent: childAgent,
					},
					{
						Type:  agent.EventTypeToolComplete,
						Agent: agent.AgentMeta{ID: "root", Depth: 0},
						Tool:  reviewTool,
						ToolResult: &llmstream.ToolResult{
							CallID: "call_review",
							Name:   "ignored_review_result",
						},
					},
					{
						Type:  agent.EventTypeDoneSuccess,
						Agent: agent.AgentMeta{ID: "root", Depth: 0},
					},
				},
				tokenUsage: llmstream.TokenUsage{
					TotalInputTokens:  12,
					TotalOutputTokens: 4,
				},
			},
		},
	}

	var buf bytes.Buffer
	session := newTestSession(Options{NoFormatting: true}, fake, &buf)
	session.formatter = verboseRecordingFormatter{}

	step, err := session.SendUserMessage(context.Background(), "review this change")
	require.NoError(t, err)
	require.Equal(t, agent.EventTypeDoneSuccess, step.TerminalEventType)

	output := buf.String()
	require.Contains(t, output, "> review this change\n")
	require.Contains(t, output, "CALL review\n")
	require.Contains(t, output, "TEXT looked at files\n")
	require.Contains(t, output, "CALL clarify_public_api\n")
	require.Contains(t, output, "TEXT checked docs\n")
	require.Contains(t, output, "DONE clarify_public_api\n")
	require.Contains(t, output, "DONE review\n")
	require.NotContains(t, output, `TEXT {"decision":"approve"}`)
	require.NotContains(t, output, "START SUBAGENT")
	require.Less(t, strings.Index(output, "TEXT looked at files\n"), strings.Index(output, "CALL clarify_public_api\n"))
	require.Less(t, strings.Index(output, "CALL clarify_public_api\n"), strings.Index(output, "TEXT checked docs\n"))
}

func TestSessionSendUserMessageSuppressesDescendantFinalAssistantTextInJSONOutput(t *testing.T) {
	t.Parallel()

	reviewTool := finalMessageBackedTestTool{name: "review"}
	clarifyTool := namedTestTool{name: "clarify_public_api"}
	childAgent := agent.AgentMeta{ID: "reviewer", Depth: 1, Parent: "root"}
	grandchildAgent := agent.AgentMeta{ID: "clarifier", Depth: 2, Parent: "reviewer"}

	fake := &fakeSessionAgent{
		sends: []fakeSessionSend{
			{
				events: []agent.Event{
					{
						Type:  agent.EventTypeToolCall,
						Agent: agent.AgentMeta{ID: "root", Depth: 0},
						Tool:  reviewTool,
						ToolCall: &llmstream.ToolCall{
							CallID: "call_review",
							Name:   "ignored_review_name",
							Type:   "function_call",
							Input:  `{"prompt":"review"}`,
						},
					},
					{
						Type:        agent.EventTypeAssistantText,
						Agent:       childAgent,
						TextContent: llmstream.TextContent{Content: "looked at files"},
					},
					{
						Type:  agent.EventTypeAssistantTurnComplete,
						Agent: childAgent,
						Turn:  textAssistantTurn("looked at files"),
					},
					{
						Type:  agent.EventTypeToolCall,
						Agent: childAgent,
						Tool:  &clarifyTool,
						ToolCall: &llmstream.ToolCall{
							CallID: "call_clarify",
							Name:   "ignored_clarify_name",
							Type:   "function_call",
							Input:  `{"path":"internal/noninteractive/SPEC.md","identifier":"Session"}`,
						},
					},
					{
						Type:  agent.EventTypeStartSubagent,
						Agent: grandchildAgent,
						StartSubagent: agent.StartSubagent{
							CallingAgentID: "reviewer",
							ToolCallID:     "call_clarify",
							Label:          "clarify_public_api",
						},
					},
					{
						Type:                    agent.EventTypeAssistantText,
						Agent:                   grandchildAgent,
						AssistantTextFinalizing: true,
						TextContent:             llmstream.TextContent{Content: "checked docs"},
					},
					{
						Type:  agent.EventTypeAssistantTurnComplete,
						Agent: grandchildAgent,
						Turn:  textAssistantTurn("checked docs"),
					},
					{
						Type:  agent.EventTypeDoneSuccess,
						Agent: grandchildAgent,
					},
					{
						Type:  agent.EventTypeToolComplete,
						Agent: childAgent,
						Tool:  &clarifyTool,
						ToolResult: &llmstream.ToolResult{
							CallID:  "call_clarify",
							Name:    "ignored_clarify_result",
							Type:    "function_call",
							Result:  `{"answer":"Session runs one step"}`,
							IsError: false,
						},
					},
					{
						Type:                    agent.EventTypeAssistantText,
						Agent:                   childAgent,
						AssistantTextFinalizing: true,
						TextContent:             llmstream.TextContent{Content: `{"decision":"approve"}`},
					},
					{
						Type:  agent.EventTypeAssistantTurnComplete,
						Agent: childAgent,
						Turn:  textAssistantTurn(`{"decision":"approve"}`),
					},
					{
						Type:  agent.EventTypeDoneSuccess,
						Agent: childAgent,
					},
					{
						Type:  agent.EventTypeToolComplete,
						Agent: agent.AgentMeta{ID: "root", Depth: 0},
						Tool:  reviewTool,
						ToolResult: &llmstream.ToolResult{
							CallID:  "call_review",
							Name:    "ignored_review_result",
							Type:    "function_call",
							Result:  "approved",
							IsError: false,
						},
					},
					{
						Type:  agent.EventTypeDoneSuccess,
						Agent: agent.AgentMeta{ID: "root", Depth: 0},
					},
				},
				tokenUsage: llmstream.TokenUsage{
					TotalInputTokens:  12,
					TotalOutputTokens: 4,
				},
			},
		},
	}

	var buf bytes.Buffer
	session := newTestSession(Options{OutputJSON: true}, fake, &buf)

	step, err := session.SendUserMessage(context.Background(), "review this change")
	require.NoError(t, err)
	require.Equal(t, agent.EventTypeDoneSuccess, step.TerminalEventType)

	require.NotContains(t, buf.String(), `{"decision":"approve"}`)

	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte{'\n'})
	require.Len(t, lines, 9)

	var visibleAssistant jsonAssistantContentEvent
	require.NoError(t, json.Unmarshal(lines[3], &visibleAssistant))
	require.Equal(t, "assistant_text", visibleAssistant.Type)
	require.Equal(t, "looked at files", visibleAssistant.Content)
	require.Equal(t, jsonAgent{ID: "reviewer", Depth: 1}, visibleAssistant.Agent)

	var childToolCall jsonToolCallEvent
	require.NoError(t, json.Unmarshal(lines[4], &childToolCall))
	require.Equal(t, "tool_call", childToolCall.Type)
	require.Equal(t, jsonTool{
		CallID: "call_clarify",
		Name:   "clarify_public_api",
		Type:   "function_call",
		Input:  `{"path":"internal/noninteractive/SPEC.md","identifier":"Session"}`,
	}, childToolCall.Tool)

	var grandchildAssistant jsonAssistantContentEvent
	require.NoError(t, json.Unmarshal(lines[5], &grandchildAssistant))
	require.Equal(t, "assistant_text", grandchildAssistant.Type)
	require.Equal(t, "checked docs", grandchildAssistant.Content)
	require.Equal(t, jsonAgent{ID: "clarifier", Depth: 2}, grandchildAssistant.Agent)

	var childToolComplete jsonToolCompleteEvent
	require.NoError(t, json.Unmarshal(lines[6], &childToolComplete))
	require.Equal(t, "tool_complete", childToolComplete.Type)
	require.Equal(t, jsonTool{
		CallID: "call_clarify",
		Name:   "clarify_public_api",
		Type:   "function_call",
	}, childToolComplete.Tool)
	require.Equal(t, jsonResult{
		Output:  `{"answer":"Session runs one step"}`,
		IsError: false,
	}, childToolComplete.Result)

	var outerToolComplete jsonToolCompleteEvent
	require.NoError(t, json.Unmarshal(lines[7], &outerToolComplete))
	require.Equal(t, "tool_complete", outerToolComplete.Type)
	require.Equal(t, jsonTool{
		CallID: "call_review",
		Name:   "review",
		Type:   "function_call",
	}, outerToolComplete.Tool)
}

func TestSessionSendUserMessageFormatsDescendantFinalAssistantTextInHumanReadableOutput(t *testing.T) {
	originalDelay := toolCallPrintDelay
	toolCallPrintDelay = 0
	t.Cleanup(func() {
		toolCallPrintDelay = originalDelay
	})

	reviewTool := finalMessageBackedTestTool{
		name: "review",
		block: llmstream.Output{
			Lines: []string{"Verdict: approve", "No actionable findings."},
		},
	}
	childAgent := agent.AgentMeta{ID: "reviewer", Depth: 1, Parent: "root"}

	fake := &fakeSessionAgent{
		sends: []fakeSessionSend{
			{
				events: []agent.Event{
					{
						Type:  agent.EventTypeToolCall,
						Agent: agent.AgentMeta{ID: "root", Depth: 0},
						Tool:  reviewTool,
						ToolCall: &llmstream.ToolCall{
							CallID: "call_review",
							Name:   "ignored_review_name",
						},
					},
					{
						Type:  agent.EventTypeStartSubagent,
						Agent: childAgent,
						StartSubagent: agent.StartSubagent{
							CallingAgentID: "root",
							ToolCallID:     "call_review",
							Label:          "review worker",
						},
					},
					{
						Type:                    agent.EventTypeAssistantText,
						Agent:                   childAgent,
						AssistantTextFinalizing: true,
						TextContent:             llmstream.TextContent{Content: `{"decision":"approve"}`},
					},
					{
						Type:  agent.EventTypeAssistantTurnComplete,
						Agent: childAgent,
						Turn:  textAssistantTurn(`{"decision":"approve"}`),
					},
					{
						Type:  agent.EventTypeDoneSuccess,
						Agent: childAgent,
					},
					{
						Type:  agent.EventTypeToolComplete,
						Agent: agent.AgentMeta{ID: "root", Depth: 0},
						Tool:  reviewTool,
						ToolResult: &llmstream.ToolResult{
							CallID: "call_review",
							Name:   "ignored_review_result",
						},
					},
					{
						Type:  agent.EventTypeDoneSuccess,
						Agent: agent.AgentMeta{ID: "root", Depth: 0},
					},
				},
			},
		},
	}

	var buf bytes.Buffer
	session := newTestSession(Options{NoFormatting: true}, fake, &buf)
	session.formatter = verboseRecordingFormatter{}

	step, err := session.SendUserMessage(context.Background(), "review this change")
	require.NoError(t, err)
	require.Equal(t, agent.EventTypeDoneSuccess, step.TerminalEventType)

	output := buf.String()
	require.Contains(t, output, "TEXT Verdict: approve\nNo actionable findings.\n")
	require.NotContains(t, output, `TEXT {"decision":"approve"}`)
}

func TestSessionSendUserMessageFormatsDescendantFinalAssistantTextInJSONOutput(t *testing.T) {
	t.Parallel()

	reviewTool := finalMessageBackedTestTool{
		name: "review",
		block: llmstream.Output{
			Lines: []string{"Verdict: approve", "No actionable findings."},
		},
	}
	childAgent := agent.AgentMeta{ID: "reviewer", Depth: 1, Parent: "root"}

	fake := &fakeSessionAgent{
		sends: []fakeSessionSend{
			{
				events: []agent.Event{
					{
						Type:  agent.EventTypeToolCall,
						Agent: agent.AgentMeta{ID: "root", Depth: 0},
						Tool:  reviewTool,
						ToolCall: &llmstream.ToolCall{
							CallID: "call_review",
							Name:   "ignored_review_name",
							Type:   "function_call",
							Input:  `{"prompt":"review"}`,
						},
					},
					{
						Type:  agent.EventTypeStartSubagent,
						Agent: childAgent,
						StartSubagent: agent.StartSubagent{
							CallingAgentID: "root",
							ToolCallID:     "call_review",
							Label:          "review worker",
						},
					},
					{
						Type:                    agent.EventTypeAssistantText,
						Agent:                   childAgent,
						AssistantTextFinalizing: true,
						TextContent:             llmstream.TextContent{Content: `{"decision":"approve"}`},
					},
					{
						Type:  agent.EventTypeAssistantTurnComplete,
						Agent: childAgent,
						Turn:  textAssistantTurn(`{"decision":"approve"}`),
					},
					{
						Type:  agent.EventTypeDoneSuccess,
						Agent: childAgent,
					},
					{
						Type:  agent.EventTypeToolComplete,
						Agent: agent.AgentMeta{ID: "root", Depth: 0},
						Tool:  reviewTool,
						ToolResult: &llmstream.ToolResult{
							CallID:  "call_review",
							Name:    "ignored_review_result",
							Type:    "function_call",
							Result:  "approved",
							IsError: false,
						},
					},
					{
						Type:  agent.EventTypeDoneSuccess,
						Agent: agent.AgentMeta{ID: "root", Depth: 0},
					},
				},
			},
		},
	}

	var buf bytes.Buffer
	session := newTestSession(Options{OutputJSON: true}, fake, &buf)

	step, err := session.SendUserMessage(context.Background(), "review this change")
	require.NoError(t, err)
	require.Equal(t, agent.EventTypeDoneSuccess, step.TerminalEventType)

	require.NotContains(t, buf.String(), `{"decision":"approve"}`)

	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte{'\n'})
	require.Len(t, lines, 6)

	var formattedAssistant jsonAssistantContentEvent
	require.NoError(t, json.Unmarshal(lines[3], &formattedAssistant))
	require.Equal(t, "assistant_text", formattedAssistant.Type)
	require.Equal(t, "Verdict: approve\nNo actionable findings.", formattedAssistant.Content)
	require.Equal(t, jsonAgent{ID: "reviewer", Depth: 1}, formattedAssistant.Agent)
}

func TestPresenterBackedTestPresenterDoesNotCustomizeSubagentFinalMessage(t *testing.T) {
	t.Parallel()

	presenter := presenterBackedTestPresenter{}
	_, ok := any(presenter).(llmstream.SubagentFinalMessagePresenter)
	require.False(t, ok)
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
	authorizer := &countingAuthorizer{sandboxDir: t.TempDir()}
	session.authorizer = authorizer
	newSessionForExec = func(_ Options) (*Session, error) {
		return session, nil
	}

	err := Exec("fix failing test", Options{NoFormatting: true})
	require.NoError(t, err)
	require.Equal(t, 1, authorizer.closeCount)
	require.Equal(t, []string{"fix failing test"}, fake.messages)
	require.Contains(t, buf.String(), "> fix failing test\n")
	require.Contains(t, buf.String(), "• Agent finished the turn. Tokens: input=7 cached_input=0 output=3 total=10")
}
