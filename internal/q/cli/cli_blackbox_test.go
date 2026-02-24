package cli_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/codalotl/codalotl/internal/q/cli"
)

type call struct {
	cmd  *cli.Command
	args []string

	in  io.Reader
	out io.Writer
	err io.Writer

	ctxValue any
}
type testContextKey struct{}

type recorder struct {
	calls []call
}

func (r *recorder) run(cmd *cli.Command, key any) cli.RunFunc {
	return func(c *cli.Context) error {
		r.calls = append(r.calls, call{
			cmd:      c.Command,
			args:     append([]string(nil), c.Args...),
			in:       c.In,
			out:      c.Out,
			err:      c.Err,
			ctxValue: c.Value(key),
		})
		return nil
	}
}

func runCLI(t *testing.T, ctx context.Context, root *cli.Command, args ...string) (code int, out string, err string) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	code = cli.Run(ctx, root, cli.Options{
		Args: args,
		In:   strings.NewReader(""),
		Out:  &outBuf,
		Err:  &errBuf,
	})
	return code, outBuf.String(), errBuf.String()
}

func requireTrailingNewline(t *testing.T, s string) {
	t.Helper()
	if s == "" {
		t.Fatalf("expected trailing newline, got empty string")
	}
	if s[len(s)-1] != '\n' {
		t.Fatalf("expected trailing newline, got %q", s)
	}
}

func requireNoANSI(t *testing.T, s string) {
	t.Helper()
	if strings.Contains(s, "\x1b") {
		t.Fatalf("expected plain text output without ANSI escapes, got %q", s)
	}
}

func requireContains(t *testing.T, s, sub string) {
	t.Helper()
	if !strings.Contains(s, sub) {
		t.Fatalf("expected output to contain %q, got %q", sub, s)
	}
}

func requireNotContains(t *testing.T, s, sub string) {
	t.Helper()
	if strings.Contains(s, sub) {
		t.Fatalf("expected output to NOT contain %q, got %q", sub, s)
	}
}

func requireIndex(t *testing.T, s, sub string) int {
	t.Helper()
	i := strings.Index(s, sub)
	if i < 0 {
		t.Fatalf("expected output to contain %q, got %q", sub, s)
	}
	return i
}

func requireOrdered(t *testing.T, s string, subs ...string) {
	t.Helper()
	last := -1
	for _, sub := range subs {
		i := requireIndex(t, s, sub)
		if i < last {
			t.Fatalf("expected ordering %q before %q in %q", subs[0], sub, s)
		}
		last = i
	}
}

func TestRun_CommandSelection_DeepestMatchAndAlias(t *testing.T) {
	const (
		rootLong = "ROOT_LONG_MARKER"
		docLong  = "DOC_LONG_MARKER"
		addLong  = "ADD_LONG_MARKER"
	)

	key := testContextKey{}
	ctx := context.WithValue(context.Background(), key, "v")

	var r recorder
	root := &cli.Command{Name: "prog", Long: rootLong}
	doc := &cli.Command{Name: "doc", Long: docLong}
	add := &cli.Command{
		Name:    "add",
		Aliases: []string{"a"},
		Long:    addLong,
		Args:    cli.ExactArgs(1),
	}
	add.Run = r.run(add, key)
	doc.AddCommand(add)
	root.AddCommand(doc)

	t.Run("deepest path", func(t *testing.T) {
		code, out, err := runCLI(t, ctx, root, "doc", "add", "x")
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d (out=%q err=%q)", code, out, err)
		}
		if len(r.calls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(r.calls))
		}
		if r.calls[0].cmd != add {
			t.Fatalf("expected handler for %q, got %q", add.Name, r.calls[0].cmd.Name)
		}
		if strings.Join(r.calls[0].args, ",") != "x" {
			t.Fatalf("expected args [x], got %v", r.calls[0].args)
		}
		if r.calls[0].ctxValue != "v" {
			t.Fatalf("expected context value %q, got %v", "v", r.calls[0].ctxValue)
		}
	})

	r.calls = nil
	t.Run("alias", func(t *testing.T) {
		code, _, _ := runCLI(t, ctx, root, "doc", "a", "x")
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		if len(r.calls) != 1 || r.calls[0].cmd != add {
			t.Fatalf("expected alias to resolve to %q, got %v", add.Name, r.calls)
		}
	})
}

func TestRun_CommandSelection_StopsAtFirstNonFlagNonChild(t *testing.T) {
	key := testContextKey{}
	ctx := context.WithValue(context.Background(), key, "v")

	var r recorder
	root := &cli.Command{Name: "prog"}
	doc := &cli.Command{Name: "doc"}
	doc.Run = r.run(doc, key)
	add := &cli.Command{Name: "add"}
	add.Run = r.run(add, key)
	doc.AddCommand(add)
	root.AddCommand(doc)

	code, out, err := runCLI(t, ctx, root, "doc", "not-a-subcommand", "add")
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d (out=%q err=%q)", code, out, err)
	}
	if len(r.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(r.calls))
	}
	if r.calls[0].cmd != doc {
		t.Fatalf("expected to execute %q, got %q", doc.Name, r.calls[0].cmd.Name)
	}
	if strings.Join(r.calls[0].args, ",") != "not-a-subcommand,add" {
		t.Fatalf("expected args [not-a-subcommand add], got %v", r.calls[0].args)
	}
}

func TestRun_DashDash_EndsSelectionAndFlagParsing(t *testing.T) {
	key := testContextKey{}
	ctx := context.WithValue(context.Background(), key, "v")

	var r recorder
	root := &cli.Command{Name: "prog"}
	root.Run = r.run(root, key)
	doc := &cli.Command{Name: "doc"}
	doc.Run = r.run(doc, key)
	add := &cli.Command{Name: "add"}
	add.Run = r.run(add, key)
	doc.AddCommand(add)
	root.AddCommand(doc)

	t.Run("ends command selection", func(t *testing.T) {
		r.calls = nil
		code, _, _ := runCLI(t, ctx, root, "--", "doc", "add")
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		if len(r.calls) != 1 || r.calls[0].cmd != root {
			t.Fatalf("expected to execute root, got %v", r.calls)
		}
		if strings.Join(r.calls[0].args, ",") != "doc,add" {
			t.Fatalf("expected args [doc add], got %v", r.calls[0].args)
		}
	})

	t.Run("treats flags and help after -- as positional", func(t *testing.T) {
		r.calls = nil
		code, _, _ := runCLI(t, ctx, root, "doc", "add", "--", "--help", "--verbose", "-v")
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		if len(r.calls) != 1 || r.calls[0].cmd != add {
			t.Fatalf("expected to execute add, got %v", r.calls)
		}
		if strings.Join(r.calls[0].args, ",") != "--help,--verbose,-v" {
			t.Fatalf("expected args [--help --verbose -v], got %v", r.calls[0].args)
		}
	})
}

func TestRun_Context_PassesCommandArgsAndIO(t *testing.T) {
	in := strings.NewReader("in")
	var outBuf, errBuf bytes.Buffer

	root := &cli.Command{Name: "prog"}
	add := &cli.Command{Name: "add"}
	root.AddCommand(add)

	called := false
	add.Run = func(c *cli.Context) error {
		called = true
		if c.Command != add {
			t.Fatalf("expected Context.Command to be %q, got %q", add.Name, c.Command.Name)
		}
		if strings.Join(c.Args, ",") != "a,b" {
			t.Fatalf("expected args [a b], got %v", c.Args)
		}
		if c.In != in || c.Out != &outBuf || c.Err != &errBuf {
			t.Fatalf("expected Context I/O to match Options I/O")
		}
		return nil
	}

	code := cli.Run(context.Background(), root, cli.Options{
		Args: []string{"add", "a", "b"},
		In:   in,
		Out:  &outBuf,
		Err:  &errBuf,
	})
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if !called {
		t.Fatalf("expected handler to be called")
	}
}

func TestRun_Flags_PlacementRules(t *testing.T) {
	const (
		rootLong = "ROOT_USAGE_MARK"
		docLong  = "DOC_USAGE_MARK"
		addLong  = "ADD_USAGE_MARK"
	)

	key := testContextKey{}
	ctx := context.WithValue(context.Background(), key, "v")

	var r recorder
	root := &cli.Command{Name: "prog", Long: rootLong}
	rootLocal := root.Flags().String("root-local", 0, "", "root local")
	verbose := root.PersistentFlags().Bool("verbose", 'v', false, "verbose")

	doc := &cli.Command{Name: "doc", Long: docLong}
	docOnly := doc.Flags().String("doc-only", 0, "", "doc only")
	docPersist := doc.PersistentFlags().Bool("doc-persist", 0, false, "doc persist")

	add := &cli.Command{Name: "add", Long: addLong, Args: cli.MinimumArgs(1)}
	addCount := add.Flags().Int("count", 'c', 0, "count")
	addName := add.Flags().String("name", 0, "", "name")
	timeout := add.Flags().Duration("timeout", 0, 0, "timeout")
	add.Run = r.run(add, key)

	doc.AddCommand(add)
	root.AddCommand(doc)

	t.Run("root persistent allowed before command path", func(t *testing.T) {
		*verbose = false
		code, _, _ := runCLI(t, ctx, root, "--verbose", "doc", "add", "x")
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		if !*verbose {
			t.Fatalf("expected --verbose to set verbose=true")
		}
	})

	t.Run("descendant persistent NOT allowed before selecting that command", func(t *testing.T) {
		*docPersist = false
		code, out, err := runCLI(t, ctx, root, "--doc-persist", "doc", "add", "x")
		if code != 2 {
			t.Fatalf("expected exit code 2, got %d (out=%q err=%q)", code, out, err)
		}
		requireContains(t, err, "--doc-persist")
	})

	t.Run("descendant persistent allowed after selecting that command", func(t *testing.T) {
		*docPersist = false
		code, _, _ := runCLI(t, ctx, root, "doc", "--doc-persist", "add", "x")
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		if !*docPersist {
			t.Fatalf("expected --doc-persist to set true")
		}
	})

	t.Run("descendant persistent allowed after selecting a deeper command", func(t *testing.T) {
		*docPersist = false
		code, _, _ := runCLI(t, ctx, root, "doc", "add", "--doc-persist", "x")
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		if !*docPersist {
			t.Fatalf("expected --doc-persist to set true")
		}
	})

	t.Run("local flag NOT allowed before selecting that command", func(t *testing.T) {
		*addCount = 0
		code, out, err := runCLI(t, ctx, root, "doc", "--count=3", "add", "x")
		if code != 2 {
			t.Fatalf("expected exit code 2, got %d (out=%q err=%q)", code, out, err)
		}
		requireContains(t, err, "--count")
	})

	t.Run("local flag allowed after selecting that command", func(t *testing.T) {
		*addCount = 0
		code, _, _ := runCLI(t, ctx, root, "doc", "add", "--count=3", "x")
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		if *addCount != 3 {
			t.Fatalf("expected count=3, got %d", *addCount)
		}
	})

	t.Run("flags may be interspersed with positional args", func(t *testing.T) {
		*addCount = 0
		*addName = ""
		r.calls = nil

		code, _, _ := runCLI(t, ctx, root, "doc", "add", "a1", "--count", "2", "a2", "--name=n1", "a3", "--name", "n2")
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		if *addCount != 2 {
			t.Fatalf("expected count=2, got %d", *addCount)
		}
		if *addName != "n2" {
			t.Fatalf("expected name=n2 (last wins), got %q", *addName)
		}
		if len(r.calls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(r.calls))
		}
		if strings.Join(r.calls[0].args, ",") != "a1,a2,a3" {
			t.Fatalf("expected args [a1 a2 a3], got %v", r.calls[0].args)
		}
	})

	t.Run("non-bool flags require an explicit value", func(t *testing.T) {
		code, out, err := runCLI(t, ctx, root, "doc", "add", "x", "--name")
		if code != 2 {
			t.Fatalf("expected exit code 2, got %d (out=%q err=%q)", code, out, err)
		}
		requireContains(t, err, "--name")
	})

	t.Run("bool flags accept explicit values and default to true when val omitted", func(t *testing.T) {
		*verbose = false
		code, _, _ := runCLI(t, ctx, root, "doc", "add", "x", "--verbose")
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		if !*verbose {
			t.Fatalf("expected --verbose to set true")
		}

		*verbose = true
		code, _, _ = runCLI(t, ctx, root, "doc", "add", "x", "--verbose=false")
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		if *verbose {
			t.Fatalf("expected --verbose=false to set false")
		}

		*verbose = true
		code, _, _ = runCLI(t, ctx, root, "doc", "add", "x", "--verbose", "false")
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		if *verbose {
			t.Fatalf("expected --verbose false to set false")
		}

		*verbose = false
		code, _, _ = runCLI(t, ctx, root, "doc", "add", "x", "-v")
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		if !*verbose {
			t.Fatalf("expected -v to set true")
		}

		*verbose = true
		code, _, _ = runCLI(t, ctx, root, "doc", "add", "x", "-v=false")
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		if *verbose {
			t.Fatalf("expected -v=false to set false")
		}
	})

	t.Run("flag values may start with '-'", func(t *testing.T) {
		neg := add.Flags().Int("neg", 0, 0, "neg")
		*neg = 0
		code, out, err := runCLI(t, ctx, root, "doc", "add", "--neg", "-1", "x")
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d (out=%q err=%q)", code, out, err)
		}
		if *neg != -1 {
			t.Fatalf("expected neg=-1, got %d", *neg)
		}
	})

	t.Run("short flags support -n=value and -n value", func(t *testing.T) {
		*addCount = 0
		code, _, _ := runCLI(t, ctx, root, "doc", "add", "-c=4", "x")
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		if *addCount != 4 {
			t.Fatalf("expected count=4, got %d", *addCount)
		}

		*addCount = 0
		code, _, _ = runCLI(t, ctx, root, "doc", "add", "-c", "5", "x")
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		if *addCount != 5 {
			t.Fatalf("expected count=5, got %d", *addCount)
		}
	})

	t.Run("if a flag is provided multiple times, the last value wins", func(t *testing.T) {
		*addCount = 0
		code, _, _ := runCLI(t, ctx, root, "doc", "add", "-c=1", "-c=2", "x")
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		if *addCount != 2 {
			t.Fatalf("expected count=2, got %d", *addCount)
		}
	})

	t.Run("duration flags parse values", func(t *testing.T) {
		*timeout = 0
		code, out, err := runCLI(t, ctx, root, "doc", "add", "--timeout=150ms", "x")
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d (out=%q err=%q)", code, out, err)
		}
		if *timeout != 150*time.Millisecond {
			t.Fatalf("expected timeout=150ms, got %v", *timeout)
		}
	})

	t.Run("root local flags are active at the start of argv", func(t *testing.T) {
		*rootLocal = ""
		code, _, _ := runCLI(t, ctx, root, "--root-local=rr", "doc", "add", "x")
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		if *rootLocal != "rr" {
			t.Fatalf("expected root-local=rr, got %q", *rootLocal)
		}
	})

	t.Run("local flags of an unselected command are unknown", func(t *testing.T) {
		*docOnly = ""
		code, _, err := runCLI(t, ctx, root, "--doc-only=zzz", "doc", "add", "x")
		if code != 2 {
			t.Fatalf("expected exit code 2, got %d (err=%q)", code, err)
		}
		requireContains(t, err, "--doc-only")
	})

	t.Run("local flags are not inherited by descendants", func(t *testing.T) {
		code, _, err := runCLI(t, ctx, root, "doc", "add", "--doc-only=zzz", "x")
		if code != 2 {
			t.Fatalf("expected exit code 2, got %d (err=%q)", code, err)
		}
		requireContains(t, err, "--doc-only")

		code, _, err = runCLI(t, ctx, root, "doc", "add", "--root-local=rr", "x")
		if code != 2 {
			t.Fatalf("expected exit code 2, got %d (err=%q)", code, err)
		}
		requireContains(t, err, "--root-local")
	})

	t.Run("to pass an arg starting with '-', use --", func(t *testing.T) {
		r.calls = nil
		code, _, err := runCLI(t, ctx, root, "doc", "add", "-x")
		if code != 2 {
			t.Fatalf("expected exit code 2, got %d (err=%q)", code, err)
		}

		r.calls = nil
		code, _, _ = runCLI(t, ctx, root, "doc", "add", "--", "-x")
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		if len(r.calls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(r.calls))
		}
		if strings.Join(r.calls[0].args, ",") != "-x" {
			t.Fatalf("expected args [-x], got %v", r.calls[0].args)
		}
	})
}

func TestRun_Help_ResolutionAndStreams(t *testing.T) {
	const (
		rootLong = "ROOT_HELP_MARK"
		docLong  = "DOC_HELP_MARK"
		addLong  = "ADD_HELP_MARK"
	)

	var r recorder
	root := &cli.Command{Name: "prog", Long: rootLong}
	doc := &cli.Command{Name: "doc", Long: docLong}
	add := &cli.Command{Name: "add", Long: addLong}
	add.Run = r.run(add, nil)
	doc.AddCommand(add)
	root.AddCommand(doc)

	t.Run("help goes to Out and does not run handlers", func(t *testing.T) {
		r.calls = nil
		code, out, err := runCLI(t, context.Background(), root, "--help", "doc", "add")
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
		if err != "" {
			t.Fatalf("expected Err empty for help, got %q", err)
		}
		requireContains(t, out, root.Name)
		requireContains(t, out, rootLong)
		requireTrailingNewline(t, out)
		requireNoANSI(t, out)
		if len(r.calls) != 0 {
			t.Fatalf("expected handler not to run on help, got %v", r.calls)
		}
	})

	t.Run("help prints for the deepest command selected so far", func(t *testing.T) {
		code, out, err := runCLI(t, context.Background(), root, "doc", "--help", "add")
		if code != 0 || err != "" {
			t.Fatalf("expected exit=0 and empty Err, got exit=%d err=%q", code, err)
		}
		requireContains(t, out, root.Name)
		requireContains(t, out, docLong)
	})

	t.Run("help prints for a leaf command when selected", func(t *testing.T) {
		code, out, err := runCLI(t, context.Background(), root, "doc", "add", "-h")
		if code != 0 || err != "" {
			t.Fatalf("expected exit=0 and empty Err, got exit=%d err=%q", code, err)
		}
		requireContains(t, out, root.Name)
		requireContains(t, out, addLong)
	})

	t.Run("after --, -h/--help are treated as positional args", func(t *testing.T) {
		r.calls = nil
		code, out, err := runCLI(t, context.Background(), root, "doc", "add", "--", "--help")
		if code != 0 {
			t.Fatalf("expected exit=0, got %d (out=%q err=%q)", code, out, err)
		}
		if err != "" {
			t.Fatalf("expected Err empty, got %q", err)
		}
		if len(r.calls) != 1 {
			t.Fatalf("expected handler call, got %v", r.calls)
		}
		if strings.Join(r.calls[0].args, ",") != "--help" {
			t.Fatalf("expected args [--help], got %v", r.calls[0].args)
		}
	})
}

func TestRun_UsageErrors_ExitCodesAndWhereUsagePrints(t *testing.T) {
	const (
		rootLong = "ROOT_USAGE_MARKER"
		docLong  = "DOC_USAGE_MARKER"
		addLong  = "ADD_USAGE_MARKER"
	)

	root := &cli.Command{Name: "prog", Long: rootLong}
	doc := &cli.Command{Name: "doc", Long: docLong}
	add := &cli.Command{Name: "add", Long: addLong, Args: func([]string) error {
		return cli.UsageError{Message: "ARG_BAD"}
	}}
	add.Run = func(*cli.Context) error { return nil }
	doc.AddCommand(add)
	root.AddCommand(doc)

	t.Run("unknown subcommand prints usage for nearest existing parent", func(t *testing.T) {
		code, out, err := runCLI(t, context.Background(), root, "doc", "nope")
		if code != 2 {
			t.Fatalf("expected exit=2, got %d (out=%q err=%q)", code, out, err)
		}
		if out != "" {
			t.Fatalf("expected Out empty on usage error, got %q", out)
		}
		requireContains(t, err, "nope")
		requireContains(t, err, docLong)
		requireTrailingNewline(t, err)
		requireNoANSI(t, err)
	})

	t.Run("unknown subcommand at root prints root usage", func(t *testing.T) {
		code, out, err := runCLI(t, context.Background(), root, "nope")
		if code != 2 {
			t.Fatalf("expected exit=2, got %d (out=%q err=%q)", code, out, err)
		}
		if out != "" {
			t.Fatalf("expected Out empty on usage error, got %q", out)
		}
		requireContains(t, err, "nope")
		requireContains(t, err, rootLong)
	})

	t.Run("namespace-only command invoked directly is a usage error", func(t *testing.T) {
		code, out, err := runCLI(t, context.Background(), root, "doc")
		if code != 2 {
			t.Fatalf("expected exit=2, got %d (out=%q err=%q)", code, out, err)
		}
		if out != "" {
			t.Fatalf("expected Out empty on usage error, got %q", out)
		}
		requireContains(t, err, docLong)
		requireContains(t, err, "subcommand")
	})

	t.Run("for namespace-only, tokens after -- are treated as unknown subcommand", func(t *testing.T) {
		code, out, err := runCLI(t, context.Background(), root, "doc", "--", "x")
		if code != 2 {
			t.Fatalf("expected exit=2, got %d (out=%q err=%q)", code, out, err)
		}
		if out != "" {
			t.Fatalf("expected Out empty on usage error, got %q", out)
		}
		requireContains(t, err, "x")
		requireContains(t, err, docLong)
	})

	t.Run("for namespace-only, even a valid child name after -- is treated as unknown subcommand", func(t *testing.T) {
		code, out, err := runCLI(t, context.Background(), root, "doc", "--", "add")
		if code != 2 {
			t.Fatalf("expected exit=2, got %d (out=%q err=%q)", code, out, err)
		}
		if out != "" {
			t.Fatalf("expected Out empty on usage error, got %q", out)
		}
		requireContains(t, err, "add")
		requireContains(t, err, docLong)
	})

	t.Run("arg validation failures are usage errors for the executed command", func(t *testing.T) {
		code, out, err := runCLI(t, context.Background(), root, "doc", "add")
		if code != 2 {
			t.Fatalf("expected exit=2, got %d (out=%q err=%q)", code, out, err)
		}
		if out != "" {
			t.Fatalf("expected Out empty on usage error, got %q", out)
		}
		requireContains(t, err, "ARG_BAD")
		requireContains(t, err, addLong)
	})

	t.Run("arg validation errors that are not UsageError still print the triggering error string", func(t *testing.T) {
		prev := add.Args
		t.Cleanup(func() { add.Args = prev })
		add.Args = func([]string) error { return errors.New("ARG_STR") }
		code, out, err := runCLI(t, context.Background(), root, "doc", "add")
		if code != 2 {
			t.Fatalf("expected exit=2, got %d (out=%q err=%q)", code, out, err)
		}
		if out != "" {
			t.Fatalf("expected Out empty on usage error, got %q", out)
		}
		requireContains(t, err, "ARG_STR")
		requireContains(t, err, addLong)
	})

	t.Run("unknown flags are usage errors for the executed command", func(t *testing.T) {
		add.Flags().Bool("known", 0, false, "")
		code, out, err := runCLI(t, context.Background(), root, "doc", "add", "--unknown")
		if code != 2 {
			t.Fatalf("expected exit=2, got %d (out=%q err=%q)", code, out, err)
		}
		if out != "" {
			t.Fatalf("expected Out empty on usage error, got %q", out)
		}
		requireContains(t, err, "--unknown")
		requireContains(t, err, addLong)
	})
}

func TestRun_HandlerErrors_DontPrintUsage(t *testing.T) {
	const addLong = "ADD_USAGE_MARKER"

	root := &cli.Command{Name: "prog"}
	add := &cli.Command{Name: "add", Long: addLong}
	root.AddCommand(add)

	t.Run("non-usage error -> exit 1 and error text only", func(t *testing.T) {
		add.Run = func(*cli.Context) error { return errors.New("boom") }
		code, out, err := runCLI(t, context.Background(), root, "add")
		if code != 1 {
			t.Fatalf("expected exit=1, got %d (out=%q err=%q)", code, out, err)
		}
		if out != "" {
			t.Fatalf("expected Out empty, got %q", out)
		}
		requireContains(t, err, "boom")
		requireNotContains(t, err, root.Name)
	})

	t.Run("ExitCoder with non-2 code preserves that exit code", func(t *testing.T) {
		add.Run = func(*cli.Context) error { return cli.ExitError{Code: 3, Err: errors.New("nope")} }
		code, out, err := runCLI(t, context.Background(), root, "add")
		if code != 3 {
			t.Fatalf("expected exit=3, got %d (out=%q err=%q)", code, out, err)
		}
		if out != "" {
			t.Fatalf("expected Out empty, got %q", out)
		}
		requireContains(t, err, "nope")
		requireNotContains(t, err, root.Name)
	})

	t.Run("ExitCoder with code 2 is treated as a usage error (prints usage)", func(t *testing.T) {
		add.Run = func(*cli.Context) error { return cli.UsageError{Message: "bad usage"} }
		code, out, err := runCLI(t, context.Background(), root, "add")
		if code != 2 {
			t.Fatalf("expected exit=2, got %d (out=%q err=%q)", code, out, err)
		}
		if out != "" {
			t.Fatalf("expected Out empty on usage error, got %q", out)
		}
		requireContains(t, err, "bad usage")
		requireContains(t, err, addLong)
	})
}

func TestHelpOutput_DeterministicOrdering(t *testing.T) {
	t.Run("subcommands sorted by Name", func(t *testing.T) {
		root := &cli.Command{Name: "prog", Long: "ROOT"}
		root.AddCommand(
			&cli.Command{Name: "cmd-zeta"},
			&cli.Command{Name: "cmd-alpha"},
			&cli.Command{Name: "cmd-beta"},
		)

		code, out, err := runCLI(t, context.Background(), root, "--help")
		if code != 0 || err != "" {
			t.Fatalf("expected exit=0 and empty Err, got exit=%d err=%q", code, err)
		}

		requireOrdered(t, out, "cmd-alpha", "cmd-beta", "cmd-zeta")
	})

	t.Run("flags sorted by long name", func(t *testing.T) {
		root := &cli.Command{Name: "prog", Long: "ROOT"}
		root.Flags().Bool("flag-zulu", 0, false, "")
		root.Flags().Bool("flag-alpha", 0, false, "")
		root.Flags().Bool("flag-beta", 0, false, "")

		code, out, err := runCLI(t, context.Background(), root, "--help")
		if code != 0 || err != "" {
			t.Fatalf("expected exit=0 and empty Err, got exit=%d err=%q", code, err)
		}

		requireOrdered(t, out, "flag-alpha", "flag-beta", "flag-zulu")
	})
}

func TestHelpOutput_HiddenCommands_NotListedButInvokable(t *testing.T) {
	const secretLong = "SECRET_LONG_MARKER"

	var r recorder
	root := &cli.Command{Name: "prog", Long: "ROOT"}
	visible := &cli.Command{Name: "visible", Short: "visible command"}
	visible.Run = r.run(visible, nil)

	secret := &cli.Command{
		Name:    "secret",
		Aliases: []string{"s"},
		Short:   "secret command",
		Long:    secretLong,
		Hidden:  true,
	}
	secret.Run = r.run(secret, nil)

	root.AddCommand(visible, secret)

	t.Run("parent help does not list hidden command", func(t *testing.T) {
		code, out, err := runCLI(t, context.Background(), root, "--help")
		if code != 0 || err != "" {
			t.Fatalf("expected exit=0 and empty Err, got exit=%d err=%q (out=%q)", code, err, out)
		}
		requireContains(t, out, "\nCommands:\n")
		requireContains(t, out, "\n  visible\tvisible command\n")
		requireNotContains(t, out, "\n  secret")
		requireNotContains(t, out, secretLong)
	})

	t.Run("hidden command can run by name", func(t *testing.T) {
		r.calls = nil
		code, out, err := runCLI(t, context.Background(), root, "secret")
		if code != 0 {
			t.Fatalf("expected exit=0, got exit=%d (out=%q err=%q)", code, out, err)
		}
		if out != "" || err != "" {
			t.Fatalf("expected no output, got out=%q err=%q", out, err)
		}
		if len(r.calls) != 1 || r.calls[0].cmd != secret {
			t.Fatalf("expected secret handler to run, got calls=%v", r.calls)
		}
	})

	t.Run("hidden command can run by alias", func(t *testing.T) {
		r.calls = nil
		code, out, err := runCLI(t, context.Background(), root, "s")
		if code != 0 {
			t.Fatalf("expected exit=0, got exit=%d (out=%q err=%q)", code, out, err)
		}
		if out != "" || err != "" {
			t.Fatalf("expected no output, got out=%q err=%q", out, err)
		}
		if len(r.calls) != 1 || r.calls[0].cmd != secret {
			t.Fatalf("expected secret handler to run, got calls=%v", r.calls)
		}
	})

	t.Run("hidden command still shows its own help when explicitly requested", func(t *testing.T) {
		code, out, err := runCLI(t, context.Background(), root, "secret", "--help")
		if code != 0 || err != "" {
			t.Fatalf("expected exit=0 and empty Err, got exit=%d err=%q (out=%q)", code, err, out)
		}
		requireContains(t, out, "prog secret")
		requireContains(t, out, secretLong)
	})
}

func TestArgsHelpers_ReturnUsageErrors(t *testing.T) {
	t.Run("NoArgs", func(t *testing.T) {
		if err := cli.NoArgs(nil); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		err := cli.NoArgs([]string{"x"})
		var ec cli.ExitCoder
		if !errors.As(err, &ec) || ec.ExitCode() != 2 {
			t.Fatalf("expected ExitCoder with code 2, got %T: %v", err, err)
		}
	})

	t.Run("ExactArgs", func(t *testing.T) {
		v := cli.ExactArgs(2)
		if err := v([]string{"a", "b"}); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		err := v([]string{"a"})
		var ec cli.ExitCoder
		if !errors.As(err, &ec) || ec.ExitCode() != 2 {
			t.Fatalf("expected ExitCoder with code 2, got %T: %v", err, err)
		}
	})

	t.Run("MinimumArgs", func(t *testing.T) {
		v := cli.MinimumArgs(2)
		if err := v([]string{"a", "b"}); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if err := v([]string{"a", "b", "c"}); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		err := v([]string{"a"})
		var ec cli.ExitCoder
		if !errors.As(err, &ec) || ec.ExitCode() != 2 {
			t.Fatalf("expected ExitCoder with code 2, got %T: %v", err, err)
		}
	})

	t.Run("RangeArgs", func(t *testing.T) {
		v := cli.RangeArgs(1, 2)
		if err := v([]string{"a"}); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if err := v([]string{"a", "b"}); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		err := v([]string{"a", "b", "c"})
		var ec cli.ExitCoder
		if !errors.As(err, &ec) || ec.ExitCode() != 2 {
			t.Fatalf("expected ExitCoder with code 2, got %T: %v", err, err)
		}
	})
}

func TestFlagTypes_AreTypedAndHaveDefaults(t *testing.T) {
	root := &cli.Command{Name: "prog"}

	b := root.Flags().Bool("bool", 0, true, "")
	s := root.Flags().String("string", 0, "d", "")
	i := root.Flags().Int("int", 0, 7, "")
	d := root.Flags().Duration("dur", 0, 123*time.Millisecond, "")

	if b == nil || s == nil || i == nil || d == nil {
		t.Fatalf("expected non-nil flag pointers")
	}
	if *b != true || *s != "d" || *i != 7 || *d != 123*time.Millisecond {
		t.Fatalf("unexpected defaults: %v %q %d %v", *b, *s, *i, *d)
	}
}
