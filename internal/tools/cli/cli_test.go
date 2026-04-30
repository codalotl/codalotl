package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/codalotl/codalotl/internal/llmstream"
	qcli "github.com/codalotl/codalotl/internal/q/cli"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInfo(t *testing.T) {
	tool := NewCodalotlCLITool(testCommandTree)

	info := tool.Info()

	assert.Equal(t, ToolNameCodalotlCLI, info.Name)
	assert.Equal(t, []string{"subcommand", "argv"}, info.Required)
	assert.Contains(t, info.Parameters, "subcommand")
	assert.Contains(t, info.Parameters, "argv")
}

func TestRunHelpRendersLeafCatalog(t *testing.T) {
	tool := NewCodalotlCLITool(testCommandTree)

	result := runCodalotlCLITool(t, tool, Params{Subcommand: "help"})

	assert.False(t, result.toolResult.IsError)
	assert.True(t, result.cliResult.Success)
	assert.Equal(t, 0, result.cliResult.ExitCode)
	assert.Equal(t, []string{"codalotl", "help"}, result.cliResult.Command)
	assert.Contains(t, result.cliResult.Stdout, "codalotl docs add")
	assert.Contains(t, result.cliResult.Stdout, "codalotl context initial")
	assert.NotContains(t, result.cliResult.Stdout, "internal-test-root")
}

func TestRunDashDashHelpRendersLeafCatalog(t *testing.T) {
	tool := NewCodalotlCLITool(testCommandTree)

	result := runCodalotlCLITool(t, tool, Params{Subcommand: "--help", Argv: []string{}})

	assert.False(t, result.toolResult.IsError)
	assert.True(t, result.cliResult.Success)
	assert.Equal(t, []string{"codalotl", "--help"}, result.cliResult.Command)
	assert.Contains(t, result.cliResult.Stdout, "codalotl docs add")
	assert.Contains(t, result.cliResult.Stdout, "codalotl context initial")
}

func TestRunNormalInvocationCapturesStdoutAndStderr(t *testing.T) {
	tool := NewCodalotlCLITool(testCommandTree)

	result := runCodalotlCLITool(t, tool, Params{
		Subcommand: "docs add",
		Argv:       []string{"--public-only", "internal/cli"},
	})

	assert.False(t, result.toolResult.IsError)
	assert.True(t, result.cliResult.Success)
	assert.Equal(t, []string{"codalotl", "docs", "add", "--public-only", "internal/cli"}, result.cliResult.Command)
	assert.Equal(t, 0, result.cliResult.ExitCode)
	assert.Equal(t, "public_only=true args=internal/cli\n", result.cliResult.Stdout)
	assert.Equal(t, "docs add stderr\n", result.cliResult.Stderr)
}

func TestRunNullArgvBehavesLikeEmptyArgv(t *testing.T) {
	tool := NewCodalotlCLITool(testCommandTree)
	call := testToolCall(`{"subcommand":"context initial","argv":null}`)

	result := tool.Run(context.Background(), call)

	assert.False(t, result.IsError)
	var cliResult Result
	require.NoError(t, json.Unmarshal([]byte(result.Result), &cliResult))
	assert.True(t, cliResult.Success)
	assert.Equal(t, []string{"codalotl", "context", "initial"}, cliResult.Command)
	assert.Equal(t, "initial context\n", cliResult.Stdout)
}

func TestRunPerCommandHelpUsesCodalotlProgramName(t *testing.T) {
	tool := NewCodalotlCLITool(testCommandTree)

	result := runCodalotlCLITool(t, tool, Params{
		Subcommand: "docs add",
		Argv:       []string{"--help"},
	})

	assert.False(t, result.toolResult.IsError)
	assert.True(t, result.cliResult.Success)
	assert.Contains(t, result.cliResult.Stdout, "codalotl docs add")
	assert.Contains(t, result.cliResult.Stdout, "--public-only")
	assert.Contains(t, result.cliResult.Stdout, "<pkg>")
	assert.NotContains(t, result.cliResult.Stdout, "internal-test-root")
}

func TestRunNonZeroExitIsACommandResultNotToolError(t *testing.T) {
	tool := NewCodalotlCLITool(testCommandTree)

	result := runCodalotlCLITool(t, tool, Params{Subcommand: "fail", Argv: []string{}})

	assert.False(t, result.toolResult.IsError)
	assert.False(t, result.cliResult.Success)
	assert.Equal(t, 7, result.cliResult.ExitCode)
	assert.Equal(t, []string{"codalotl", "fail"}, result.cliResult.Command)
	assert.Contains(t, result.cliResult.Stderr, "before failure")
	assert.Contains(t, result.cliResult.Stderr, "intentional failure")
}

func TestRunPropagatesContextCancellation(t *testing.T) {
	tool := NewCodalotlCLITool(cancellationCommandTree)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	input, err := json.Marshal(Params{Subcommand: "wait", Argv: []string{}})
	require.NoError(t, err)
	toolResult := tool.Run(ctx, testToolCall(string(input)))

	require.False(t, toolResult.IsError)
	var cliResult Result
	require.NoError(t, json.Unmarshal([]byte(toolResult.Result), &cliResult))
	assert.True(t, cliResult.Success)
	assert.Equal(t, "context canceled\n", cliResult.Stdout)
}

func TestRunEmptySubcommandIsUsageErrorResult(t *testing.T) {
	tool := NewCodalotlCLITool(testCommandTree)

	result := runCodalotlCLITool(t, tool, Params{Subcommand: "", Argv: []string{}})

	assert.False(t, result.toolResult.IsError)
	assert.False(t, result.cliResult.Success)
	assert.Equal(t, 2, result.cliResult.ExitCode)
	assert.Equal(t, []string{"codalotl"}, result.cliResult.Command)
	assert.Contains(t, result.cliResult.Stderr, "empty subcommand")
}

func TestRunMalformedParamsAreToolErrors(t *testing.T) {
	tool := NewCodalotlCLITool(testCommandTree)

	result := tool.Run(context.Background(), testToolCall(`{"subcommand":1,"argv":[]}`))

	assert.True(t, result.IsError)
	assert.Contains(t, result.Result, "malformed codalotl_cli params")
	assert.Error(t, result.SourceErr)
}

func TestRunFactoryFailureIsToolError(t *testing.T) {
	tool := NewCodalotlCLITool(func() *qcli.Command { return nil })

	result := tool.Run(context.Background(), testToolCall(`{"subcommand":"context initial","argv":[]}`))

	assert.True(t, result.IsError)
	assert.Contains(t, result.Result, "factory returned nil")
	assert.Error(t, result.SourceErr)
}

func TestRunUsesFreshCommandTreeForEachInvocation(t *testing.T) {
	var calls int
	tool := NewCodalotlCLITool(func() *qcli.Command {
		calls++
		return commandTreeWithPingOutput(fmt.Sprintf("fresh tree %d\n", calls))
	})

	first := runCodalotlCLITool(t, tool, Params{Subcommand: "ping", Argv: []string{}})
	second := runCodalotlCLITool(t, tool, Params{Subcommand: "ping", Argv: []string{}})

	assert.Equal(t, 2, calls)
	assert.Equal(t, "fresh tree 1\n", first.cliResult.Stdout)
	assert.Equal(t, "fresh tree 2\n", second.cliResult.Stdout)
}

func TestPresenterShowsStartAndFinishOnly(t *testing.T) {
	tool := NewCodalotlCLITool(testCommandTree)
	presenter := tool.Presenter()
	call := testToolCall(`{"subcommand":"docs add","argv":["--public-only","internal/cli"]}`)

	start := presenter.Present(call, nil)
	finish := presenter.Present(call, &llmstream.ToolResult{
		Result: `{"success":true,"stdout":"do not show me","stderr":"do not show me"}`,
	})

	assert.Equal(t, llmstream.CompletionBehaviorReplace, start.Behavior)
	assert.Equal(t, "Running codalotl docs add --public-only internal/cli", lineText(start.Summary))
	assert.Nil(t, start.Body)
	assert.Equal(t, "Ran codalotl docs add --public-only internal/cli", lineText(finish.Summary))
	assert.Nil(t, finish.Body)
}

type runResult struct {
	toolResult llmstream.ToolResult
	cliResult  Result
}

func runCodalotlCLITool(t *testing.T, tool llmstream.Tool, params Params) runResult {
	t.Helper()

	input, err := json.Marshal(params)
	require.NoError(t, err)

	toolResult := tool.Run(context.Background(), testToolCall(string(input)))
	require.False(t, toolResult.IsError)

	var cliResult Result
	require.NoError(t, json.Unmarshal([]byte(toolResult.Result), &cliResult))
	return runResult{toolResult: toolResult, cliResult: cliResult}
}

func testToolCall(input string) llmstream.ToolCall {
	return llmstream.ToolCall{
		CallID: "call-test",
		Name:   ToolNameCodalotlCLI,
		Type:   "function_call",
		Input:  input,
	}
}

func testCommandTree() *qcli.Command {
	root := &qcli.Command{Name: "internal-test-root"}

	docs := &qcli.Command{Name: "docs", Short: "Documentation commands."}
	add := &qcli.Command{
		Name:    "add",
		Short:   "Add missing documentation comments.",
		Long:    "Add missing documentation comments to a package.",
		Usage:   "[--public-only] <pkg>",
		ArgHelp: []qcli.ArgHelp{{Display: "<pkg>", Description: "Package to document."}},
		Args:    qcli.ExactArgs(1),
	}
	publicOnly := add.Flags().Bool("public-only", 0, false, "Only document exported identifiers.")
	add.Run = func(c *qcli.Context) error {
		fmt.Fprintf(c.Out, "public_only=%v args=%s\n", *publicOnly, strings.Join(c.Args, ","))
		fmt.Fprint(c.Err, "docs add stderr\n")
		return nil
	}
	docs.AddCommand(add)

	contextCmd := &qcli.Command{Name: "context", Short: "Context commands."}
	initial := &qcli.Command{
		Name:             "initial",
		Short:            "Print initial context.",
		NoPositionalArgs: true,
		Args:             qcli.NoArgs,
		Run: func(c *qcli.Context) error {
			fmt.Fprint(c.Out, "initial context\n")
			return nil
		},
	}
	contextCmd.AddCommand(initial)

	fail := &qcli.Command{
		Name:             "fail",
		Short:            "Fail intentionally.",
		NoPositionalArgs: true,
		Args:             qcli.NoArgs,
		Run: func(c *qcli.Context) error {
			fmt.Fprint(c.Err, "before failure\n")
			return qcli.ExitError{Code: 7, Err: errors.New("intentional failure")}
		},
	}

	root.AddCommand(docs, contextCmd, fail)
	return root
}

func commandTreeWithPingOutput(output string) *qcli.Command {
	root := &qcli.Command{Name: "internal-test-root"}
	ping := &qcli.Command{
		Name:             "ping",
		Short:            "Ping.",
		NoPositionalArgs: true,
		Args:             qcli.NoArgs,
		Run: func(c *qcli.Context) error {
			fmt.Fprint(c.Out, output)
			return nil
		},
	}
	root.AddCommand(ping)
	return root
}

func cancellationCommandTree() *qcli.Command {
	root := &qcli.Command{Name: "internal-test-root"}
	wait := &qcli.Command{
		Name:             "wait",
		Short:            "Wait.",
		NoPositionalArgs: true,
		Args:             qcli.NoArgs,
		Run: func(c *qcli.Context) error {
			select {
			case <-c.Context.Done():
				fmt.Fprint(c.Out, "context canceled\n")
				return nil
			default:
				return qcli.ExitError{Code: 9, Err: errors.New("context was not canceled")}
			}
		},
	}
	root.AddCommand(wait)
	return root
}

func lineText(line llmstream.Line) string {
	parts := make([]string, 0, len(line.Segments))
	for _, segment := range line.Segments {
		parts = append(parts, segment.Text)
	}
	if line.JoinWithSpace {
		return strings.Join(parts, " ")
	}
	return strings.Join(parts, "")
}
