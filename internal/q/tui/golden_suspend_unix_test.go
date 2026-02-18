//go:build !windows

package tui_test

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/codalotl/codalotl/internal/q/tui"
	"github.com/stretchr/testify/require"
)

const suspendHelperEnv = "TUI_SUSPEND_HELPER"

const (
	helperReady      = "helper:ready"
	helperSuspending = "helper:suspending"
	helperResumed    = "helper:resumed"
	helperDone       = "helper:done"
)

// TestSuspend exercises Suspend by running a helper process in its own process group so the SIGTSTP sent by Suspend only affects the helper.
func TestSuspend(t *testing.T) {
	// NOTE: not necessary to check `if runtime.GOOS == "windows"` because this compiles for non-windows.

	cmd := exec.Command(os.Args[0], "-test.run=TestSuspendHelperProcess$")
	cmd.Env = append(os.Environ(), suspendHelperEnv+"=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	stderr, err := cmd.StderrPipe()
	require.NoError(t, err)

	var stderrBuf bytes.Buffer
	stderrDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(&stderrBuf, stderr)
		close(stderrDone)
	}()

	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		if cmd.ProcessState == nil && cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		<-stderrDone
	})

	reader := bufio.NewReader(stdout)
	readLine := func(expected string) {
		line, err := reader.ReadString('\n')
		require.NoErrorf(t, err, "stderr:\n%s", stderrBuf.String())
		require.Equalf(t, expected, strings.TrimSpace(line), "stderr:\n%s", stderrBuf.String())
	}

	readLine(helperReady)
	readLine(helperSuspending)

	time.Sleep(50 * time.Millisecond)
	require.NoError(t, cmd.Process.Signal(syscall.SIGCONT))

	readLine(helperResumed)
	readLine(helperDone)

	require.NoErrorf(t, cmd.Wait(), "stderr:\n%s", stderrBuf.String())
	<-stderrDone
}

func TestSuspendHelperProcess(t *testing.T) {
	if os.Getenv(suspendHelperEnv) != "1" {
		return
	}

	input, output := requireTestTTY(t)
	fmt.Println(helperReady)

	model := &suspendTestModel{}
	if err := tui.RunTUI(model, tui.Options{
		Input:  input,
		Output: output,
	}); err != nil {
		t.Fatalf("RunTUI failed: %v", err)
	}

	fmt.Println(helperDone)
}

type suspendTestModel struct{}

func (m *suspendTestModel) Init(t *tui.TUI) {
	go func() {
		time.Sleep(50 * time.Millisecond)
		fmt.Println(helperSuspending)
		t.Suspend()
	}()
}

func (m *suspendTestModel) Update(t *tui.TUI, msg tui.Message) {
	switch msg.(type) {
	case tui.SigResumeEvent:
		fmt.Println(helperResumed)
		t.Quit()
	case tui.SigTermEvent:
		// allow RunTUI to exit cleanly
	}
}

func (m *suspendTestModel) View() string {
	return ""
}
