package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/codalotl/codalotl/internal/llmstream"
	qcli "github.com/codalotl/codalotl/internal/q/cli"
)

const ToolNameCodalotlCLI = "codalotl_cli"

// CommandTreeFunc returns a fresh whitelisted codalotl command tree.
type CommandTreeFunc func() *qcli.Command

// NewCodalotlCLITool creates the codalotl_cli tool.
func NewCodalotlCLITool(newCommandTree CommandTreeFunc) llmstream.Tool {
	return &codalotlCLITool{newCommandTree: newCommandTree}
}

// Params are the codalotl_cli tool parameters.
type Params struct {
	Subcommand string   `json:"subcommand"`
	Argv       []string `json:"argv"`
}

// Result is the machine-readable codalotl_cli tool result.
type Result struct {
	Success  bool     `json:"success"`
	Command  []string `json:"command"`
	ExitCode int      `json:"exit_code"`
	Stdout   string   `json:"stdout"`
	Stderr   string   `json:"stderr"`
}

type codalotlCLITool struct {
	newCommandTree CommandTreeFunc
}

func (t *codalotlCLITool) Info() llmstream.ToolInfo {
	return llmstream.ToolInfo{
		Name: ToolNameCodalotlCLI,
		Description: "Run whitelisted codalotl CLI commands in-process. " +
			"Use subcommand \"help\" or \"--help\" to list available commands. " +
			"Pass command flags and positional args in argv; pass per-command --help in argv for detailed help.",
		Parameters: map[string]any{
			"subcommand": map[string]any{
				"type":        "string",
				"description": "Command path after codalotl, such as \"context initial\" or \"docs add\". Use \"help\" or \"--help\" to list available commands.",
			},
			"argv": map[string]any{
				"type":        []string{"array", "null"},
				"description": "Flags and args for the subcommand. Null behaves like an empty array.",
				"items": map[string]any{
					"type": "string",
				},
			},
		},
		Required: []string{"subcommand", "argv"},
	}
}

func (t *codalotlCLITool) Name() string {
	return ToolNameCodalotlCLI
}

func (t *codalotlCLITool) Presenter() llmstream.Presenter {
	return codalotlCLIPresenter{}
}

func (t *codalotlCLITool) Run(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
	params, err := parseParams(call.Input)
	if err != nil {
		return errorToolResult(call, fmt.Sprintf("malformed %s params: %v", ToolNameCodalotlCLI, err), err)
	}

	tokens := strings.Fields(params.Subcommand)
	command := commandVector(tokens, params.Argv)
	if len(tokens) == 0 {
		return jsonToolResult(call, Result{
			Success:  false,
			Command:  command,
			ExitCode: 2,
			Stderr:   "usage error: empty subcommand\n",
		})
	}

	root, err := t.freshCommandTree()
	if err != nil {
		return errorToolResult(call, err.Error(), err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if isCatalogHelp(tokens) {
		qcli.WriteHelp(&stdout, root, root, qcli.HelpOptions{LeafCommands: true})
		return jsonToolResult(call, Result{
			Success:  true,
			Command:  command,
			ExitCode: 0,
			Stdout:   stdout.String(),
			Stderr:   stderr.String(),
		})
	}

	args := append([]string(nil), tokens...)
	args = append(args, params.Argv...)
	exitCode := qcli.Run(ctx, root, qcli.Options{
		Args: args,
		Out:  &stdout,
		Err:  &stderr,
	})

	return jsonToolResult(call, Result{
		Success:  exitCode == 0,
		Command:  command,
		ExitCode: exitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
	})
}

func (t *codalotlCLITool) freshCommandTree() (root *qcli.Command, err error) {
	if t.newCommandTree == nil {
		return nil, errors.New("codalotl_cli command tree factory is nil")
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("codalotl_cli command tree factory panicked: %v", recovered)
		}
	}()
	root = t.newCommandTree()
	if root == nil {
		return nil, errors.New("codalotl_cli command tree factory returned nil")
	}
	root.Name = "codalotl"
	return root, nil
}

func parseParams(input string) (Params, error) {
	var raw map[string]json.RawMessage
	decoder := json.NewDecoder(strings.NewReader(input))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		return Params{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Params{}, errors.New("multiple JSON values")
	}

	for key := range raw {
		if key != "subcommand" && key != "argv" {
			return Params{}, fmt.Errorf("unknown field %q", key)
		}
	}
	if _, ok := raw["subcommand"]; !ok {
		return Params{}, errors.New("missing required field \"subcommand\"")
	}
	if _, ok := raw["argv"]; !ok {
		return Params{}, errors.New("missing required field \"argv\"")
	}

	var params Params
	if err := json.Unmarshal(raw["subcommand"], &params.Subcommand); err != nil {
		return Params{}, fmt.Errorf("subcommand: %w", err)
	}
	if string(raw["argv"]) != "null" {
		if err := json.Unmarshal(raw["argv"], &params.Argv); err != nil {
			return Params{}, fmt.Errorf("argv: %w", err)
		}
	}
	return params, nil
}

func isCatalogHelp(tokens []string) bool {
	return len(tokens) == 1 && (tokens[0] == "help" || tokens[0] == "--help")
}

func commandVector(subcommandTokens []string, argv []string) []string {
	command := []string{"codalotl"}
	command = append(command, subcommandTokens...)
	command = append(command, argv...)
	return command
}

func jsonToolResult(call llmstream.ToolCall, result Result) llmstream.ToolResult {
	body, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return errorToolResult(call, fmt.Sprintf("failed to encode %s result: %v", ToolNameCodalotlCLI, err), err)
	}
	return llmstream.ToolResult{
		CallID:  call.CallID,
		Name:    call.Name,
		Type:    call.Type,
		Result:  string(body),
		IsError: false,
	}
}

func errorToolResult(call llmstream.ToolCall, msg string, srcErr error) llmstream.ToolResult {
	return llmstream.ToolResult{
		CallID:    call.CallID,
		Name:      call.Name,
		Type:      call.Type,
		Result:    msg,
		IsError:   true,
		SourceErr: srcErr,
	}
}

type codalotlCLIPresenter struct{}

func (codalotlCLIPresenter) Present(call llmstream.ToolCall, result *llmstream.ToolResult) llmstream.Presentation {
	action := "Running"
	if result != nil {
		action = "Ran"
	}
	return llmstream.Presentation{
		Behavior: llmstream.CompletionBehaviorReplace,
		Summary: llmstream.Line{
			Segments: []llmstream.Segment{{
				Text: action + " " + presentationCommand(call),
				Role: llmstream.RoleAction,
			}},
		},
	}
}

func presentationCommand(call llmstream.ToolCall) string {
	params, err := parseParams(call.Input)
	if err != nil {
		return ToolNameCodalotlCLI
	}
	return shellCommandString(commandVector(strings.Fields(params.Subcommand), params.Argv))
}

func shellCommandString(command []string) string {
	parts := make([]string, 0, len(command))
	for _, part := range command {
		parts = append(parts, shellQuote(part))
	}
	return strings.Join(parts, " ")
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\r\n'\"\\$`|&;<>(){}[]*?!") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
