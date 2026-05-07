package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/codalotl/codalotl/internal/agentbuilder"
	"github.com/codalotl/codalotl/internal/agentregistry"
	"github.com/codalotl/codalotl/internal/llmstream"
	toolcli "github.com/codalotl/codalotl/internal/tools/cli"
	toolrefactor "github.com/codalotl/codalotl/internal/tools/refactor"
	"github.com/codalotl/codalotl/internal/tools/toolsetinterface"
	"github.com/stretchr/testify/require"
)

func TestRun_InstallsCodalotlCLIToolOverride(t *testing.T) {
	isolateUserConfig(t)

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "--help"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.NotEmpty(t, out.String())
	require.Empty(t, errOut.String())

	reg, err := agentbuilder.BuildRegistry()
	require.NoError(t, err)
	require.Contains(t, reg.ListToolNames(), toolcli.ToolNameCodalotlCLI)

	generic, ok := reg.Lookup(agentbuilder.AgentGeneric)
	require.True(t, ok)
	toolNames := collectAgentToolNames(t, generic)
	require.Contains(t, toolNames, toolcli.ToolNameCodalotlCLI)

	for _, agentName := range []string{
		agentbuilder.AgentPackageModeNoContext,
		agentbuilder.AgentPackageModeDefaultContext,
		agentbuilder.AgentLimitedPackageMode,
	} {
		def, ok := reg.Lookup(agentName)
		require.True(t, ok)
		require.NotContains(t, def.ToolNames, toolcli.ToolNameCodalotlCLI)
	}
}

func TestRun_InstallsRefactorToolOverride(t *testing.T) {
	isolateUserConfig(t)

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "--help"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.NotEmpty(t, out.String())
	require.Empty(t, errOut.String())

	reg, err := agentbuilder.BuildRegistry()
	require.NoError(t, err)
	require.Contains(t, reg.ListToolNames(), toolrefactor.ToolNameRefactor)

	generic, ok := reg.Lookup(agentbuilder.AgentGeneric)
	require.True(t, ok)
	require.Contains(t, collectAgentToolNames(t, generic), toolrefactor.ToolNameRefactor)

	for _, agentName := range []string{
		agentbuilder.AgentPackageModeNoContext,
		agentbuilder.AgentPackageModeDefaultContext,
		agentbuilder.AgentLimitedPackageMode,
	} {
		def, ok := reg.Lookup(agentName)
		require.True(t, ok)
		require.NotContains(t, collectAgentToolNames(t, def), toolrefactor.ToolNameRefactor)
	}
}

func TestCodalotlCLITool_OnlyExposesDocsAdd(t *testing.T) {
	tool := toolcli.NewCodalotlCLITool(newCodalotlCLICommandTree)

	helpResult := decodeCodalotlCLIToolResult(t, tool.Run(context.Background(), llmstream.ToolCall{
		CallID: "call-help",
		Name:   toolcli.ToolNameCodalotlCLI,
		Type:   "function_call",
		Input:  `{"subcommand":"help","argv":[]}`,
	}))
	require.True(t, helpResult.Success)
	require.Equal(t, 0, helpResult.ExitCode)
	require.Contains(t, helpResult.Stdout, "codalotl docs add")
	require.NotContains(t, helpResult.Stdout, "codalotl docs reflow")
	require.NotContains(t, helpResult.Stdout, "codalotl context public")

	detailedHelp := decodeCodalotlCLIToolResult(t, tool.Run(context.Background(), llmstream.ToolCall{
		CallID: "call-docs-add-help",
		Name:   toolcli.ToolNameCodalotlCLI,
		Type:   "function_call",
		Input:  `{"subcommand":"docs add","argv":["--help"]}`,
	}))
	require.True(t, detailedHelp.Success)
	require.Equal(t, 0, detailedHelp.ExitCode)
	require.Contains(t, detailedHelp.Stdout, "codalotl docs add")
	require.Contains(t, detailedHelp.Stdout, "--public-only")
	require.Contains(t, detailedHelp.Stdout, "--important")
	require.Contains(t, detailedHelp.Stdout, "--include-test")

	rejected := decodeCodalotlCLIToolResult(t, tool.Run(context.Background(), llmstream.ToolCall{
		CallID: "call-disallowed",
		Name:   toolcli.ToolNameCodalotlCLI,
		Type:   "function_call",
		Input:  `{"subcommand":"context initial","argv":["internal/cli"]}`,
	}))
	require.False(t, rejected.Success)
	require.Equal(t, 2, rejected.ExitCode)
	require.Contains(t, rejected.Stderr, "unknown subcommand")
	require.Contains(t, rejected.Stderr, "context")
}

func collectAgentToolNames(t *testing.T, def agentregistry.Definition) []string {
	t.Helper()

	toolNames := append([]string(nil), def.ToolNames...)
	if def.ToolsBuilder != nil {
		dynamicToolNames, err := def.ToolsBuilder(toolsetinterface.Options{
			AgentName: def.Name,
		})
		require.NoError(t, err)
		toolNames = append(toolNames, dynamicToolNames...)
	}
	return toolNames
}

func decodeCodalotlCLIToolResult(t *testing.T, result llmstream.ToolResult) toolcli.Result {
	t.Helper()

	require.False(t, result.IsError)

	var decoded toolcli.Result
	require.NoError(t, json.Unmarshal([]byte(result.Result), &decoded))
	return decoded
}
