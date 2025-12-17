package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func runCLI(t *testing.T, root *Command, args []string) (int, string, string) {
	t.Helper()
	var out bytes.Buffer
	var errOut bytes.Buffer
	code := Run(context.Background(), root, Options{
		Args: args,
		Out:  &out,
		Err:  &errOut,
	})
	return code, out.String(), errOut.String()
}

func TestRun_SelectsDeepestCommandAndParsesFlagsInterspersed(t *testing.T) {
	root := &Command{Name: "prog"}
	verbose := root.PersistentFlags().Bool("verbose", 'v', false, "Enable verbose logging")

	doc := &Command{Name: "doc", Short: "Documentation tools"}
	add := &Command{
		Name: "add",
		Args: ExactArgs(1),
	}
	mode := add.Flags().String("mode", 'm', "", "Mode")

	var gotArgs []string
	add.Run = func(c *Context) error {
		gotArgs = append([]string(nil), c.Args...)
		return nil
	}

	doc.AddCommand(add)
	root.AddCommand(doc)

	code, stdout, stderr := runCLI(t, root, []string{"--verbose", "doc", "add", "pkg", "--mode=fast"})
	if code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if stdout != "" || stderr != "" {
		t.Fatalf("expected no output; stdout=%q stderr=%q", stdout, stderr)
	}
	if !*verbose {
		t.Fatalf("expected verbose=true")
	}
	if *mode != "fast" {
		t.Fatalf("expected mode=fast, got %q", *mode)
	}
	if len(gotArgs) != 1 || gotArgs[0] != "pkg" {
		t.Fatalf("expected args=[pkg], got %v", gotArgs)
	}
}

func TestRun_CommandSelectionStopsOnFirstRealArg(t *testing.T) {
	root := &Command{Name: "prog"}

	doc := &Command{Name: "doc"}
	add := &Command{Name: "add"}
	doc.AddCommand(add)
	root.AddCommand(doc)

	var ran string
	doc.Run = func(c *Context) error {
		ran = "doc"
		if strings.Join(c.Args, ",") != "file,add" {
			t.Fatalf("unexpected args: %v", c.Args)
		}
		return nil
	}
	add.Run = func(*Context) error {
		ran = "add"
		return nil
	}

	code, stdout, stderr := runCLI(t, root, []string{"doc", "file", "add"})
	if code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if ran != "doc" {
		t.Fatalf("expected doc to run, ran=%q", ran)
	}
}

func TestRun_HelpPrintsForDeepestSelectedCommandSoFar(t *testing.T) {
	root := &Command{Name: "prog"}
	doc := &Command{Name: "doc", Short: "Docs"}
	root.AddCommand(doc)

	code, stdout, stderr := runCLI(t, root, []string{"doc", "-h"})
	if code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("expected no stderr, got %q", stderr)
	}
	if !strings.Contains(stdout, "prog doc") {
		t.Fatalf("expected help to mention command path; stdout=%q", stdout)
	}
	if !strings.HasSuffix(stdout, "\n") {
		t.Fatalf("expected trailing newline; stdout=%q", stdout)
	}
}

func TestRun_UnknownFlagIsUsageErrorAndIncludesToken(t *testing.T) {
	root := &Command{Name: "prog"}
	doc := &Command{Name: "doc"}
	add := &Command{Name: "add", Run: func(*Context) error { return nil }}
	doc.AddCommand(add)
	root.AddCommand(doc)

	code, stdout, stderr := runCLI(t, root, []string{"doc", "add", "--nope"})
	if code != 2 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if stdout != "" {
		t.Fatalf("expected no stdout, got %q", stdout)
	}
	if !strings.Contains(stderr, "unknown flag: --nope") {
		t.Fatalf("expected stderr to mention unknown token; stderr=%q", stderr)
	}
	if !strings.Contains(stderr, "prog doc add") {
		t.Fatalf("expected usage for selected command; stderr=%q", stderr)
	}
}

func TestRun_LocalFlagBeforeCommandIsUnknown(t *testing.T) {
	root := &Command{Name: "prog"}
	doc := &Command{Name: "doc"}
	add := &Command{Name: "add", Run: func(*Context) error { return nil }}
	add.Flags().String("mode", 'm', "", "Mode")
	doc.AddCommand(add)
	root.AddCommand(doc)

	code, stdout, stderr := runCLI(t, root, []string{"--mode=fast", "doc", "add"})
	if code != 2 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if stdout != "" {
		t.Fatalf("expected no stdout, got %q", stdout)
	}
	if !strings.Contains(stderr, "unknown flag: --mode=fast") {
		t.Fatalf("expected stderr to include token; stderr=%q", stderr)
	}
	if !strings.Contains(stderr, "Usage:") || !strings.Contains(stderr, "  prog") {
		t.Fatalf("expected root usage; stderr=%q", stderr)
	}
}

func TestRun_NamespaceOnlyCommandRequiresSubcommand(t *testing.T) {
	root := &Command{Name: "prog"}
	doc := &Command{Name: "doc"} // Run nil: namespace-only
	doc.AddCommand(&Command{Name: "add", Run: func(*Context) error { return nil }})
	root.AddCommand(doc)

	code, stdout, stderr := runCLI(t, root, []string{"doc"})
	if code != 2 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if stdout != "" {
		t.Fatalf("expected no stdout, got %q", stdout)
	}
	if !strings.Contains(stderr, "missing required subcommand") {
		t.Fatalf("expected missing subcommand message; stderr=%q", stderr)
	}
	if !strings.Contains(stderr, "prog doc") {
		t.Fatalf("expected usage for namespace command; stderr=%q", stderr)
	}
}

func TestRun_NamespaceOnlyTreatsRemainingTokenAsUnknownSubcommand(t *testing.T) {
	root := &Command{Name: "prog"}
	doc := &Command{Name: "doc"} // Run nil: namespace-only
	doc.AddCommand(&Command{Name: "add", Run: func(*Context) error { return nil }})
	root.AddCommand(doc)

	code, stdout, stderr := runCLI(t, root, []string{"doc", "--", "-h"})
	if code != 2 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if stdout != "" {
		t.Fatalf("expected no stdout, got %q", stdout)
	}
	if !strings.Contains(stderr, "unknown subcommand: -h") {
		t.Fatalf("expected unknown subcommand message; stderr=%q", stderr)
	}
}

func TestRun_HandlerErrorDoesNotPrintUsage(t *testing.T) {
	root := &Command{
		Name: "prog",
		Run: func(*Context) error {
			return errors.New("boom")
		},
	}

	code, stdout, stderr := runCLI(t, root, nil)
	if code != 1 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if stdout != "" {
		t.Fatalf("expected no stdout, got %q", stdout)
	}
	if strings.Contains(stderr, "Usage:") {
		t.Fatalf("expected no usage on handler error; stderr=%q", stderr)
	}
	if strings.TrimSpace(stderr) != "boom" {
		t.Fatalf("expected error message; stderr=%q", stderr)
	}
}

func TestRun_HandlerUsageErrorPrintsUsage(t *testing.T) {
	root := &Command{Name: "prog"}
	root.Run = func(*Context) error {
		return UsageError{Message: "bad input"}
	}

	code, stdout, stderr := runCLI(t, root, nil)
	if code != 2 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if stdout != "" {
		t.Fatalf("expected no stdout, got %q", stdout)
	}
	if !strings.Contains(stderr, "bad input") || !strings.Contains(stderr, "Usage:") {
		t.Fatalf("expected usage error message and usage; stderr=%q", stderr)
	}
}
