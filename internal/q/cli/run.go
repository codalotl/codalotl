package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

type Options struct {
	// Args is the argv excluding the program name (typically os.Args[1:]).
	Args []string

	// In/Out/Err override standard I/O. If nil, defaults are used.
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

// Context is passed to a command handler.
//
// Positional args are in Args. Flag values are typically read via variables bound
// at command construction time (e.g. fs.Bool(...)).
type Context struct {
	context.Context

	Command *Command
	Args    []string

	In  io.Reader
	Out io.Writer
	Err io.Writer
}

// Run executes a command tree as a CLI program and returns a process exit code.
func Run(ctx context.Context, root *Command, opts Options) int {
	if root == nil {
		panic("cli: Run called with nil root")
	}
	if root.Name == "" {
		panic("cli: Run called with root.Name empty")
	}

	in := opts.In
	if in == nil {
		in = os.Stdin
	}
	out := opts.Out
	if out == nil {
		out = os.Stdout
	}
	errOut := opts.Err
	if errOut == nil {
		errOut = os.Stderr
	}

	selected, args, parseErr := parseArgv(root, opts.Args, out)
	if parseErr != nil {
		if errors.Is(parseErr, errHelpPrinted) {
			return 0
		}
		printUsageError(root, selected, parseErr, errOut)
		return 2
	}

	if selected.Run == nil {
		if len(args) == 0 {
			printUsageError(root, selected, usageErrorf("missing required subcommand"), errOut)
			return 2
		}
		printUsageError(root, selected, usageErrorf("unknown subcommand: %s", args[0]), errOut)
		return 2
	}

	if selected.Args != nil {
		if err := selected.Args(args); err != nil {
			return exitForArgsError(root, selected, err, errOut)
		}
	}

	c := &Context{
		Context: ctx,
		Command: selected,
		Args:    args,
		In:      in,
		Out:     out,
		Err:     errOut,
	}
	if err := selected.Run(c); err != nil {
		return exitForHandlerError(root, selected, err, errOut)
	}
	return 0
}

var errHelpPrinted = errors.New("help printed")

func parseArgv(root *Command, argv []string, out io.Writer) (*Command, []string, error) {
	selected := root
	selectionEnded := false
	parsingEnded := false
	var positional []string

	for i := 0; i < len(argv); i++ {
		token := argv[i]

		if parsingEnded {
			positional = append(positional, argv[i:]...)
			break
		}

		if token == "--" {
			parsingEnded = true
			selectionEnded = true
			continue
		}

		if token == "-h" || token == "--help" {
			writeHelp(out, root, selected)
			return selected, nil, errHelpPrinted
		}

		if isFlagToken(token) {
			active := selected.activeFlags()
			consumed, err := parseFlagToken(active, token, argv, i)
			if err != nil {
				return selected, nil, err
			}
			i += consumed
			continue
		}

		if !selectionEnded {
			if child := selected.childByToken(token); child != nil {
				selected = child
				continue
			}
			selectionEnded = true
		}

		positional = append(positional, token)
	}
	return selected, positional, nil
}

func isFlagToken(token string) bool {
	return strings.HasPrefix(token, "-") && token != "-" // "-" is a valid positional arg.
}

func parseFlagToken(active activeFlags, token string, argv []string, idx int) (int, error) {
	nextValue, hasNext := nextTokenValue(argv, idx)
	hasDashDash := hasNext && nextValue == "--"
	nextPtr := (*string)(nil)
	if hasNext {
		nextPtr = &nextValue
	}

	// Long flag: --name or --name=value
	if strings.HasPrefix(token, "--") {
		name, value, hasValue := splitFlagValue(token[2:])
		var valuePtr *string
		if hasValue {
			valuePtr = &value
		}
		consumeNext, err := active.parseAndSet(token, hasDashDash, name, 0, valuePtr, nextPtr)
		if err != nil {
			return 0, err
		}
		if consumeNext {
			return 1, nil
		}
		return 0, nil
	}

	// Short flag: -n or -n=value, or single-dash long flag: -name or -name=value
	if len(token) >= 3 && token[2] != '=' {
		name, value, hasValue := splitFlagValue(token[1:])
		var valuePtr *string
		if hasValue {
			valuePtr = &value
		}
		consumeNext, err := active.parseAndSet(token, hasDashDash, name, 0, valuePtr, nextPtr)
		if err != nil {
			return 0, err
		}
		if consumeNext {
			return 1, nil
		}
		return 0, nil
	}

	if len(token) < 2 {
		return 0, usageErrorf("unknown flag: %s", token)
	}
	shorthand := rune(token[1])
	var valuePtr *string
	if len(token) >= 3 && token[2] == '=' {
		v := token[3:]
		valuePtr = &v
	}
	consumeNext, err := active.parseAndSet(token, hasDashDash, "", shorthand, valuePtr, nextPtr)
	if err != nil {
		return 0, err
	}
	if consumeNext {
		return 1, nil
	}
	return 0, nil
}

func splitFlagValue(s string) (name, value string, ok bool) {
	if i := strings.IndexByte(s, '='); i >= 0 {
		return s[:i], s[i+1:], true
	}
	return s, "", false
}

func nextTokenValue(argv []string, idx int) (string, bool) {
	if idx+1 >= len(argv) {
		return "", false
	}
	return argv[idx+1], true
}

func exitForHandlerError(root, cmd *Command, err error, errOut io.Writer) int {
	var ec ExitCoder
	if errors.As(err, &ec) {
		code := ec.ExitCode()
		if code == 2 {
			printUsageError(root, cmd, err, errOut)
			return 2
		}
		if code == 0 {
			return 0
		}
		if msg := err.Error(); msg != "" {
			fmt.Fprintln(errOut, msg)
		}
		return code
	}

	if msg := err.Error(); msg != "" {
		fmt.Fprintln(errOut, msg)
	}
	return 1
}

func exitForArgsError(root, cmd *Command, err error, errOut io.Writer) int {
	var ec ExitCoder
	if errors.As(err, &ec) {
		code := ec.ExitCode()
		if code == 2 {
			printUsageError(root, cmd, err, errOut)
			return 2
		}
		if code == 0 {
			return 0
		}
		if msg := err.Error(); msg != "" {
			fmt.Fprintln(errOut, msg)
		}
		return code
	}

	printUsageError(root, cmd, err, errOut)
	return 2
}

func printUsageError(root, cmd *Command, err error, errOut io.Writer) {
	msg := usageErrorMessage(err)
	if msg != "" {
		fmt.Fprintln(errOut, msg)
		fmt.Fprintln(errOut)
	}
	writeHelp(errOut, root, cmd)
}

func usageErrorMessage(err error) string {
	var ue UsageError
	if errors.As(err, &ue) && ue.Message != "" {
		return ue.Message
	}
	if err == nil {
		return ""
	}
	if errors.Is(err, errHelpPrinted) {
		return ""
	}
	return err.Error()
}
