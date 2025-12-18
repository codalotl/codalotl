package noninteractive

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/codalotl/codalotl/internal/llmstream"
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

	want := "• Agent finished the turn. Session tokens: input=10 cached_input=3 output=7 total=20"
	if line != want {
		t.Fatalf("got %q, want %q", line, want)
	}
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
