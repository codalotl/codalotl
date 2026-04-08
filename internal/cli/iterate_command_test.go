package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/iterate"
	"github.com/codalotl/codalotl/internal/noninteractive"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func stubNewNoninteractiveSession(t *testing.T, fn func(noninteractive.Options) (iterateSession, error)) {
	t.Helper()

	orig := newNoninteractiveSession
	newNoninteractiveSession = fn
	t.Cleanup(func() { newNoninteractiveSession = orig })
}

func stubIterateInterruptContext(t *testing.T, fn func(context.Context) (context.Context, context.CancelFunc)) {
	t.Helper()

	orig := newIterateInterruptContext
	newIterateInterruptContext = fn
	t.Cleanup(func() { newIterateInterruptContext = orig })
}

func stubNoninteractiveIsPrinted(t *testing.T, fn func(error) bool) {
	t.Helper()

	orig := noninteractiveIsPrinted
	noninteractiveIsPrinted = fn
	t.Cleanup(func() { noninteractiveIsPrinted = orig })
}

func stubRunIterateLoop(t *testing.T, fn func(context.Context, iterate.Runner, iterate.Options) (iterate.Result, error)) {
	t.Helper()

	orig := runIterateLoop
	runIterateLoop = fn
	t.Cleanup(func() { runIterateLoop = orig })
}

type fakeIterateSession struct {
	t          *testing.T
	results    []noninteractive.Result
	errs       []error
	onSend     func(ctx context.Context, prompt string, call int)
	closeErr   error
	sends      []string
	closeCount int
}

func (s *fakeIterateSession) SendUserMessage(ctx context.Context, userPrompt string) (noninteractive.Result, error) {
	s.sends = append(s.sends, userPrompt)
	call := len(s.sends) - 1
	if s.onSend != nil {
		s.onSend(ctx, userPrompt, call)
	}
	if call >= len(s.results) {
		s.t.Fatalf("unexpected SendUserMessage call %d with prompt %q", call+1, userPrompt)
	}
	var err error
	if call < len(s.errs) {
		err = s.errs[call]
	}
	return s.results[call], err
}

func (s *fakeIterateSession) Close() error {
	s.closeCount++
	return s.closeErr
}

func TestRun_Iterate_HelpMentionsFlags(t *testing.T) {
	isolateUserConfig(t)

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "iterate", "--help"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Contains(t, out.String(), "--prompt-file")
	require.Contains(t, out.String(), "--orchestrate")
	require.Contains(t, out.String(), "--max-steps")
	require.Contains(t, out.String(), "--max-minutes")
	require.Contains(t, out.String(), "--decision-prompt")
	require.Contains(t, out.String(), "--continue-mode")
	require.Contains(t, out.String(), "--yes")
	require.Contains(t, out.String(), "--json")
	require.Contains(t, out.String(), "--model")
	require.Contains(t, out.String(), "--no-color")
	require.Contains(t, out.String(), "--slash-command")
	require.NotContains(t, out.String(), "--package")
	require.Empty(t, errOut.String())
}

func TestRun_Iterate_PromptFileLoadsPromptAndUsesExecLikeConfigDefaults(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()
	writeProjectConfig(t, tmp, "{\n  \"autoyes\": true,\n  \"preferredmodel\": \"gpt-5.4-high\"\n}\n")
	chdirForTest(t, tmp)

	promptPath := filepath.Join(tmp, "prompt.md")
	promptContents := "first line\nsecond line\n"
	require.NoError(t, os.WriteFile(promptPath, []byte(promptContents), 0o644))

	var gotOpts noninteractive.Options
	session := &fakeIterateSession{
		t: t,
		results: []noninteractive.Result{{
			TerminalEventType:   agent.EventTypeDoneSuccess,
			FinalAssistantText:  "STOP_ITERATION",
			ContextUsagePercent: 9,
		}},
	}
	stubNewNoninteractiveSession(t, func(opts noninteractive.Options) (iterateSession, error) {
		gotOpts = opts
		return session, nil
	})

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "iterate", "--prompt-file", promptPath}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Equal(t, []string{promptContents}, session.sends)
	require.True(t, gotOpts.AutoYes)
	require.Equal(t, "gpt-5.4-high", string(gotOpts.ModelID))
	require.Empty(t, errOut.String())
}

func TestRun_Iterate_OrchestrateAllowsNoPrompt(t *testing.T) {
	isolateUserConfig(t)
	chdirForTest(t, t.TempDir())

	var gotOpts noninteractive.Options
	session := &fakeIterateSession{
		t: t,
		results: []noninteractive.Result{{
			TerminalEventType:   agent.EventTypeDoneSuccess,
			FinalAssistantText:  "STOP_ITERATION",
			ContextUsagePercent: 4,
		}},
	}
	stubNewNoninteractiveSession(t, func(opts noninteractive.Options) (iterateSession, error) {
		gotOpts = opts
		return session, nil
	})

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "iterate", "--orchestrate"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Equal(t, []string{""}, session.sends)
	require.Equal(t, "orchestrate", gotOpts.SlashCommand)
	require.Empty(t, errOut.String())
}

func TestRun_Iterate_ValidatesPromptSources(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()
	chdirForTest(t, tmp)

	promptPath := filepath.Join(tmp, "prompt.txt")
	require.NoError(t, os.WriteFile(promptPath, []byte("hello from file"), 0o644))

	called := false
	stubNewNoninteractiveSession(t, func(opts noninteractive.Options) (iterateSession, error) {
		called = true
		return &fakeIterateSession{t: t}, nil
	})

	t.Run("prompt-file-and-args", func(t *testing.T) {
		var out bytes.Buffer
		var errOut bytes.Buffer
		code, err := Run([]string{"codalotl", "iterate", "--prompt-file", promptPath, "hello"}, &RunOptions{Out: &out, Err: &errOut})
		require.Error(t, err)
		require.Equal(t, 2, code)
		require.Contains(t, errOut.String(), "either <prompt> or --prompt-file")
		require.False(t, called)
	})

	t.Run("no-prompt-without-session-starting-slash-command", func(t *testing.T) {
		var out bytes.Buffer
		var errOut bytes.Buffer
		code, err := Run([]string{"codalotl", "iterate"}, &RunOptions{Out: &out, Err: &errOut})
		require.Error(t, err)
		require.Equal(t, 2, code)
		require.Contains(t, errOut.String(), "prompt is required")
		require.False(t, called)
	})
}

func TestRun_Iterate_FreshAndResumeSessions(t *testing.T) {
	isolateUserConfig(t)
	chdirForTest(t, t.TempDir())

	t.Run("resume", func(t *testing.T) {
		var sessions []*fakeIterateSession
		stubNewNoninteractiveSession(t, func(opts noninteractive.Options) (iterateSession, error) {
			session := &fakeIterateSession{
				t: t,
				results: []noninteractive.Result{
					{
						TerminalEventType:   agent.EventTypeDoneSuccess,
						FinalAssistantText:  "CONTINUE_ITERATION",
						ContextUsagePercent: 10,
					},
					{
						TerminalEventType:   agent.EventTypeDoneSuccess,
						FinalAssistantText:  "STOP_ITERATION",
						ContextUsagePercent: 14,
					},
				},
			}
			sessions = append(sessions, session)
			return session, nil
		})

		var out bytes.Buffer
		var errOut bytes.Buffer
		code, err := Run([]string{"codalotl", "iterate", "--continue-mode=resume", "fix it"}, &RunOptions{Out: &out, Err: &errOut})
		require.NoError(t, err)
		require.Equal(t, 0, code)
		require.Len(t, sessions, 1)
		require.Equal(t, []string{"fix it", iterateResumePrompt}, sessions[0].sends)
		require.Equal(t, 1, sessions[0].closeCount)
		require.Empty(t, errOut.String())
	})

	t.Run("fresh", func(t *testing.T) {
		var sessions []*fakeIterateSession
		stubNewNoninteractiveSession(t, func(opts noninteractive.Options) (iterateSession, error) {
			session := &fakeIterateSession{
				t: t,
				results: []noninteractive.Result{{
					TerminalEventType:   agent.EventTypeDoneSuccess,
					FinalAssistantText:  "STOP_ITERATION",
					ContextUsagePercent: 8,
				}},
			}
			if len(sessions) == 0 {
				session.results[0].FinalAssistantText = "CONTINUE_ITERATION"
			}
			sessions = append(sessions, session)
			return session, nil
		})

		var out bytes.Buffer
		var errOut bytes.Buffer
		code, err := Run([]string{"codalotl", "iterate", "--continue-mode=fresh", "fix it"}, &RunOptions{Out: &out, Err: &errOut})
		require.NoError(t, err)
		require.Equal(t, 0, code)
		require.Len(t, sessions, 2)
		require.Equal(t, []string{"fix it"}, sessions[0].sends)
		require.Equal(t, []string{"fix it"}, sessions[1].sends)
		require.Equal(t, 1, sessions[0].closeCount)
		require.Equal(t, 1, sessions[1].closeCount)
		require.Empty(t, errOut.String())
	})
}

func TestRun_Iterate_RetryExhaustedReturnsNonZero(t *testing.T) {
	isolateUserConfig(t)
	chdirForTest(t, t.TempDir())

	stubRunIterateLoop(t, func(ctx context.Context, runner iterate.Runner, opts iterate.Options) (iterate.Result, error) {
		return iterate.Result{
			Iterations: 3,
			StopReason: iterate.StopReasonRetryExhausted,
			LastStep: iterate.StepResult{
				TerminalEventType:   agent.EventTypeError,
				ContextUsagePercent: 42,
			},
		}, nil
	})

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "iterate", "hello"}, &RunOptions{Out: &out, Err: &errOut})
	require.Error(t, err)
	require.Equal(t, 1, code)
	require.Contains(t, err.Error(), "retry exhaustion")
	require.Contains(t, errOut.String(), "retry exhaustion")
	require.Contains(t, out.String(), "iterate: stopped after 3 iteration(s) (reason=retry_exhausted, event=error)")
}

func TestRun_Iterate_FreshModeClosesReplacedAndFinalSessions(t *testing.T) {
	isolateUserConfig(t)
	chdirForTest(t, t.TempDir())

	var sessions []*fakeIterateSession
	stubNewNoninteractiveSession(t, func(opts noninteractive.Options) (iterateSession, error) {
		session := &fakeIterateSession{
			t: t,
			results: []noninteractive.Result{{
				TerminalEventType:   agent.EventTypeDoneSuccess,
				FinalAssistantText:  "CONTINUE_ITERATION",
				ContextUsagePercent: 10,
			}},
		}
		if len(sessions) > 0 {
			session.results[0].FinalAssistantText = "STOP_ITERATION"
		}
		sessions = append(sessions, session)
		return session, nil
	})
	stubRunIterateLoop(t, func(ctx context.Context, runner iterate.Runner, opts iterate.Options) (iterate.Result, error) {
		_, err := runner.RunStep(ctx, iterate.Step{
			Iteration: 1,
			Kind:      iterate.StepKindPrompt,
			Mode:      iterate.ContinueModeFresh,
			Prompt:    opts.Prompt,
		})
		require.NoError(t, err)

		lastStep, err := runner.RunStep(ctx, iterate.Step{
			Iteration: 2,
			Kind:      iterate.StepKindPrompt,
			Mode:      iterate.ContinueModeFresh,
			Prompt:    opts.Prompt,
		})
		require.NoError(t, err)

		return iterate.Result{
			Iterations: 2,
			StopReason: iterate.StopReasonDone,
			LastStep:   lastStep,
		}, nil
	})

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "iterate", "--continue-mode=fresh", "hello"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Len(t, sessions, 2)
	require.Equal(t, []string{"hello"}, sessions[0].sends)
	require.Equal(t, []string{"hello"}, sessions[1].sends)
	require.Equal(t, 1, sessions[0].closeCount)
	require.Equal(t, 1, sessions[1].closeCount)
	require.Empty(t, errOut.String())
}

func TestRun_Iterate_TextModeLifecycleMetadata(t *testing.T) {
	isolateUserConfig(t)
	chdirForTest(t, t.TempDir())

	stubNewNoninteractiveSession(t, func(opts noninteractive.Options) (iterateSession, error) {
		return &fakeIterateSession{
			t: t,
			results: []noninteractive.Result{{
				TerminalEventType:   agent.EventTypeDoneSuccess,
				FinalAssistantText:  "STOP_ITERATION",
				ContextUsagePercent: 7,
			}},
			onSend: func(ctx context.Context, prompt string, call int) {
				_, err := io.WriteString(opts.Out, "agent output\n")
				require.NoError(t, err)
			},
		}, nil
	})

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "iterate", "hello"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())

	stdout := out.String()
	startIdx := strings.Index(stdout, "iterate: iteration 1 prompt starting (mode=fresh)")
	agentIdx := strings.Index(stdout, "agent output")
	finishIdx := strings.Index(stdout, "iterate: iteration 1 prompt finished (event=done_success, context=7%)")
	completeIdx := strings.Index(stdout, "iterate: stopped after 1 iteration(s) (reason=done, event=done_success)")
	require.NotEqual(t, -1, startIdx)
	require.NotEqual(t, -1, agentIdx)
	require.NotEqual(t, -1, finishIdx)
	require.NotEqual(t, -1, completeIdx)
	require.Less(t, startIdx, agentIdx)
	require.Less(t, agentIdx, finishIdx)
	require.Less(t, finishIdx, completeIdx)
}

func TestRun_Iterate_JSONModeLifecycleMetadata(t *testing.T) {
	isolateUserConfig(t)
	chdirForTest(t, t.TempDir())

	stubNewNoninteractiveSession(t, func(opts noninteractive.Options) (iterateSession, error) {
		require.True(t, opts.OutputJSON)
		return &fakeIterateSession{
			t: t,
			results: []noninteractive.Result{{
				TerminalEventType:   agent.EventTypeDoneSuccess,
				FinalAssistantText:  "STOP_ITERATION",
				ContextUsagePercent: 12,
			}},
			onSend: func(ctx context.Context, prompt string, call int) {
				_, err := io.WriteString(opts.Out, "{\"type\":\"assistant_text\",\"content\":\"hello\"}\n")
				require.NoError(t, err)
			},
		}, nil
	})

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "iterate", "--json", "hello"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	require.Len(t, lines, 4)

	var events []map[string]any
	for _, line := range lines {
		var event map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &event))
		events = append(events, event)
	}

	require.Equal(t, "iterate_step_start", events[0]["type"])
	require.Equal(t, float64(1), events[0]["iteration"])
	require.Equal(t, "prompt", events[0]["step_kind"])
	require.Equal(t, "fresh", events[0]["mode"])

	require.Equal(t, "assistant_text", events[1]["type"])

	require.Equal(t, "iterate_step_finish", events[2]["type"])
	require.Equal(t, "done_success", events[2]["terminal_event_type"])
	require.Equal(t, float64(12), events[2]["context_usage_percent"])

	require.Equal(t, "iterate_complete", events[3]["type"])
	require.Equal(t, "done", events[3]["stop_reason"])
	require.Equal(t, "done_success", events[3]["terminal_event_type"])
}

func TestRun_Iterate_PrintedTerminalErrorIsRetried(t *testing.T) {
	isolateUserConfig(t)
	chdirForTest(t, t.TempDir())

	printedErr := errors.New("printed boom")
	stubNoninteractiveIsPrinted(t, func(err error) bool {
		return errors.Is(err, printedErr)
	})

	var sessions []*fakeIterateSession
	stubNewNoninteractiveSession(t, func(opts noninteractive.Options) (iterateSession, error) {
		session := &fakeIterateSession{
			t: t,
			results: []noninteractive.Result{{
				TerminalEventType:   agent.EventTypeDoneSuccess,
				FinalAssistantText:  "STOP_ITERATION",
				ContextUsagePercent: 6,
			}},
		}
		if len(sessions) == 0 {
			session.results[0] = noninteractive.Result{
				TerminalEventType:   agent.EventTypeError,
				FinalAssistantText:  "partial output",
				ContextUsagePercent: 11,
			}
			session.errs = []error{printedErr}
		}
		sessions = append(sessions, session)
		return session, nil
	})

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "iterate", "--continue-mode=fresh", "hello"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Len(t, sessions, 2)
	assert.Equal(t, []string{"hello"}, sessions[0].sends)
	assert.Equal(t, []string{"hello"}, sessions[1].sends)
	assert.Contains(t, out.String(), "iterate: iteration 1 prompt finished (event=error, context=11%)")
	assert.Contains(t, out.String(), "iterate: iteration 2 prompt finished (event=done_success, context=6%)")
	assert.Contains(t, out.String(), "iterate: stopped after 2 iteration(s) (reason=done, event=done_success)")
	assert.Empty(t, errOut.String())
}

func TestRun_Iterate_PrintedCanceledStepStopsWholeCommand(t *testing.T) {
	isolateUserConfig(t)
	chdirForTest(t, t.TempDir())

	printedErr := errors.New("printed canceled")
	stubNoninteractiveIsPrinted(t, func(err error) bool {
		return errors.Is(err, printedErr)
	})

	var cancel context.CancelFunc
	stubIterateInterruptContext(t, func(parent context.Context) (context.Context, context.CancelFunc) {
		ctx, stop := context.WithCancel(parent)
		cancel = stop
		return ctx, stop
	})

	var sessions []*fakeIterateSession
	stubNewNoninteractiveSession(t, func(opts noninteractive.Options) (iterateSession, error) {
		session := &fakeIterateSession{
			t: t,
			results: []noninteractive.Result{{
				TerminalEventType:   agent.EventTypeCanceled,
				ContextUsagePercent: 3,
			}},
			errs: []error{printedErr},
			onSend: func(ctx context.Context, prompt string, call int) {
				cancel()
			},
		}
		sessions = append(sessions, session)
		return session, nil
	})

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "iterate", "hello"}, &RunOptions{Out: &out, Err: &errOut})
	require.Error(t, err)
	require.Equal(t, 1, code)
	assert.Contains(t, err.Error(), "interrupted")
	require.Len(t, sessions, 1)
	assert.Equal(t, []string{"hello"}, sessions[0].sends)
	assert.Contains(t, out.String(), "iterate: iteration 1 prompt finished (event=canceled, context=3%)")
	assert.Contains(t, out.String(), "iterate: stopped after 1 iteration(s) (event=canceled, error=interrupted)")
	assert.NotContains(t, out.String(), "iterate: iteration 2")
}
