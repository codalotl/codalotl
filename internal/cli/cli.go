package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"

	qcli "github.com/codalotl/codalotl/internal/q/cli"
)

// Version is the codalotl version. It is a var (not a const) so build tooling can override it (for example via `-ldflags "-X .../internal/cli.Version=1.2.3"`).
//
// NOTE: our current build system does not do this - we just bump versions with ./bump_release.sh, which edits this source file.
var Version = "0.11.0"

// In/Out/Err override standard I/O. If nil, defaults are used. Overriding is useful for testing.
//
// Note that if Stdout/Stderr are overridden, we will pass them to other package's functions if they accept them. However, not all will; some packages will probably
// print to Stdout.
type RunOptions struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

// Run runs the CLI with args (typically you'd use os.Args).
//
// It returns a recommended exit code (0, 1, or 2) and an error, if any:
//   - 0 -> err == nil
//   - 1 -> err != nil, but the structure of args is sound (flags are correct, etc).
//   - 2 -> err != nil, args parse error or misuse of flags, etc.
//
// Note that in cases of errors, Run has already displayed an error message to opts.Err || Stderr. Callers may use os.Exit with the exit code.
func Run(args []string, opts *RunOptions) (int, error) {
	argv := args
	if len(argv) > 0 {
		argv = argv[1:]
	}

	root, runState := newRootCommand(!hasHelpFlag(argv))

	var in io.Reader = os.Stdin
	var out io.Writer = os.Stdout
	var errW io.Writer = os.Stderr
	if opts != nil {
		if opts.In != nil {
			in = opts.In
		}
		if opts.Out != nil {
			out = opts.Out
		}
		if opts.Err != nil {
			errW = opts.Err
		}
	}

	// internal/q/cli intentionally returns only an exit code, so we tee stderr to
	// produce a non-nil error when exitCode != 0.
	var stderrBuf bytes.Buffer
	var stdoutBuf bytes.Buffer
	outTee := io.MultiWriter(out, &stdoutBuf)
	errTee := io.MultiWriter(errW, &stderrBuf)

	exitCode := qcli.Run(context.Background(), root, qcli.Options{
		Args: argv,
		In:   in,
		Out:  outTee,
		Err:  errTee,
	})

	if exitCode == 0 {
		return 0, nil
	}

	// Prefer stderr as the primary error message, but fall back to stdout in case
	// something printed there (ex: some packages write errors to stdout).
	msg := strings.TrimSpace(stderrBuf.String())
	if msg == "" {
		msg = strings.TrimSpace(stdoutBuf.String())
	}
	if msg == "" {
		msg = "command failed"
	}

	if exitCode == 1 && runState != nil && !runState.getPanicked() {
		_ = reportErrorForExitCode1(runState.getMonitor(), runState.getEvent(), msg)
	}

	return exitCode, errors.New(msg)
}

func hasHelpFlag(argv []string) bool {
	for _, a := range argv {
		if a == "-h" || a == "--help" {
			return true
		}
	}
	return false
}
