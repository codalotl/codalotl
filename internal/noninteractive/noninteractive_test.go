package noninteractive

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
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

	line := formatAgentFinishedTurnLine(llmstream.TokenUsage{
		TotalInputTokens:  10,
		CachedInputTokens: 3,
		TotalOutputTokens: 7,
	})

	want := "• Agent finished the turn. Tokens: input=7 cached_input=3 output=7 total=17"
	if line != want {
		t.Fatalf("got %q, want %q", line, want)
	}
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
	completedAssistantTurns := []llmstream.Turn{
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
	}
	actualUsage := llmstream.TokenUsage{
		TotalInputTokens:  100,
		CachedInputTokens: 40,
		TotalOutputTokens: 7,
	}

	withoutIdeal := buildDoneSuccessReport(actualTurns, completedAssistantTurns, actualUsage, false)
	withIdeal := buildDoneSuccessReport(actualTurns, completedAssistantTurns, actualUsage, true)

	require.Contains(t, withoutIdeal.UsageAndCaching, "resp_actual")
	require.Equal(t, withoutIdeal.UsageAndCaching, withIdeal.UsageAndCaching)

	require.Len(t, withoutIdeal.Lines, 1)
	require.Equal(t, "• Agent finished the turn. Tokens: input=60 cached_input=40 output=7 total=107", withoutIdeal.Lines[0])

	require.Len(t, withIdeal.Lines, 2)
	require.Equal(t, "• actual token usage: input=60 cached_input=40 output=7 total=107", withIdeal.Lines[0])
	require.Equal(t, "• Agent finished the turn. Tokens: input=14 cached_input=10 output=3 total=27", withIdeal.Lines[1])
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
