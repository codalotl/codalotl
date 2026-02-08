package cmdrunner

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestResultToXMLSingleCommandSuccess(t *testing.T) {
	t.Parallel()

	result := Result{
		Results: []CommandResult{
			{
				Command:    "go",
				Args:       []string{"test", "./pkg"},
				Output:     "PASS\nok\taxi/q/cmdrunner 0.003s\n",
				ExecStatus: ExecStatusCompleted,
				ExitCode:   0,
				Outcome:    OutcomeSuccess,
				Duration:   2 * time.Second,
			},
		},
	}

	got := result.ToXML("test-status")
	want := `<test-status ok="true">
$ go test ./pkg
PASS
ok	axi/q/cmdrunner 0.003s
</test-status>`

	require.Equal(t, want, got)
}

func TestResultToXMLSingleCommandFailureAttributes(t *testing.T) {
	t.Parallel()

	result := Result{
		Results: []CommandResult{
			{
				Command:    "go",
				Args:       []string{"test"},
				Output:     "FAIL\n",
				ExecStatus: ExecStatusCompleted,
				ExitCode:   2,
				Outcome:    OutcomeFailed,
			},
		},
	}

	got := result.ToXML("test-status")
	want := `<test-status ok="false" exit-code="2">
$ go test
FAIL
</test-status>`

	require.Equal(t, want, got)
}

func TestResultToXMLShowsExecStatusAndDuration(t *testing.T) {
	t.Parallel()

	result := Result{
		Results: []CommandResult{
			{
				Command:    "go",
				Args:       []string{"test", "./pkg"},
				Output:     "context deadline exceeded",
				ExecStatus: ExecStatusTimedOut,
				ExitCode:   -1,
				Signal:     "TERM",
				Outcome:    OutcomeFailed,
				Duration:   7200 * time.Millisecond,
			},
		},
	}

	got := result.ToXML("test-status")
	want := `<test-status ok="false" exec-status="timed_out" exit-code="-1" signal="TERM" duration="7.2s">
$ go test ./pkg
context deadline exceeded
</test-status>`

	require.Equal(t, want, got)
}

func TestResultToXMLMultipleCommands(t *testing.T) {
	t.Parallel()

	result := Result{
		Results: []CommandResult{
			{
				Command:    "gofmt",
				Args:       []string{"-l", "./q/cmdrunner"},
				ExecStatus: ExecStatusCompleted,
				ExitCode:   0,
				Outcome:    OutcomeSuccess,
				Duration:   500 * time.Millisecond,
			},
			{
				Command:    "gochecklint",
				Args:       []string{"-l", "./q/cmdrunner"},
				Output:     "Found 2 issues:\n- issue1\n- issue2\n",
				ExecStatus: ExecStatusCompleted,
				ExitCode:   1,
				Outcome:    OutcomeFailed,
			},
		},
	}

	got := result.ToXML("lint-status")
	want := `<lint-status ok="false">
<command ok="true">
$ gofmt -l ./q/cmdrunner
</command>
<command ok="false">
$ gochecklint -l ./q/cmdrunner
Found 2 issues:
- issue1
- issue2
</command>
</lint-status>`

	require.Equal(t, want, got)
}

func TestResultToXMLMessageIfNoOutput(t *testing.T) {
	t.Parallel()

	result := Result{
		Results: []CommandResult{
			{
				Command:           "gofmt",
				Args:              []string{"-l", "./pkg"},
				ExecStatus:        ExecStatusCompleted,
				ExitCode:          0,
				Outcome:           OutcomeSuccess,
				MessageIfNoOutput: "no lint issues",
			},
		},
	}

	got := result.ToXML("lint-status")
	want := `<lint-status ok="true" message="no lint issues">
$ gofmt -l ./pkg
</lint-status>`

	require.Equal(t, want, got)
}

func TestResultToXMLShowCWD_Single(t *testing.T) {
	t.Parallel()

	cwd := t.TempDir()
	result := Result{
		Results: []CommandResult{
			{
				Command:    "go",
				Args:       []string{"version"},
				CWD:        cwd,
				ShowCWD:    true,
				ExecStatus: ExecStatusCompleted,
				ExitCode:   0,
				Outcome:    OutcomeSuccess,
			},
		},
	}

	got := result.ToXML("cmd-status")
	want := fmt.Sprintf(`<cmd-status ok="true" cwd="%s">
$ go version
</cmd-status>`, cwd)

	require.Equal(t, want, got)
}

func TestResultToXMLShowCWD_Multiple(t *testing.T) {
	t.Parallel()

	cwd1 := t.TempDir()
	cwd2 := t.TempDir()
	result := Result{
		Results: []CommandResult{
			{
				Command:    "echo",
				Args:       []string{"hello"},
				CWD:        cwd1,
				ShowCWD:    true,
				ExecStatus: ExecStatusCompleted,
				ExitCode:   0,
				Outcome:    OutcomeSuccess,
			},
			{
				Command:    "echo",
				Args:       []string{"world"},
				CWD:        cwd2,
				ShowCWD:    true,
				ExecStatus: ExecStatusCompleted,
				ExitCode:   0,
				Outcome:    OutcomeSuccess,
			},
		},
	}

	got := result.ToXML("echoes")
	want := fmt.Sprintf(`<echoes ok="true">
<command ok="true" cwd="%s">
$ echo hello
</command>
<command ok="true" cwd="%s">
$ echo world
</command>
</echoes>`, cwd1, cwd2)

	require.Equal(t, want, got)
}

func TestResultToXMLAttrs_Single(t *testing.T) {
	t.Parallel()

	result := Result{
		Results: []CommandResult{
			{
				Command:    "echo",
				Args:       []string{"hello"},
				ExecStatus: ExecStatusCompleted,
				ExitCode:   0,
				Outcome:    OutcomeSuccess,
				Attrs:      []string{"dryrun", "true", "lang", "go"},
			},
		},
	}

	got := result.ToXML("cmd-status")
	want := `<cmd-status ok="true" dryrun="true" lang="go">
$ echo hello
</cmd-status>`

	require.Equal(t, want, got)
}

func TestResultToXMLAttrs_Multiple(t *testing.T) {
	t.Parallel()

	result := Result{
		Results: []CommandResult{
			{
				Command:    "echo",
				Args:       []string{"hello"},
				ExecStatus: ExecStatusCompleted,
				ExitCode:   0,
				Outcome:    OutcomeSuccess,
				Attrs:      []string{"dryrun", "true"},
			},
			{
				Command:    "echo",
				Args:       []string{"world"},
				ExecStatus: ExecStatusCompleted,
				ExitCode:   0,
				Outcome:    OutcomeSuccess,
				Attrs:      []string{"lang", "go", "phase", "2"},
			},
		},
	}

	got := result.ToXML("echoes")
	want := `<echoes ok="true">
<command ok="true" dryrun="true">
$ echo hello
</command>
<command ok="true" lang="go" phase="2">
$ echo world
</command>
</echoes>`

	require.Equal(t, want, got)
}
