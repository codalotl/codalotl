package exttools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/codalotl/codalotl/internal/lints"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/q/cmdrunner"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/codalotl/codalotl/internal/tools/coretools"
)

//go:embed run_tests.md
var descriptionRunTests string

// ToolNameRunTests is the registered tool name for the package test tool.
const ToolNameRunTests = "run_tests"

var runTestsPresenterInstance llmstream.Presenter = runTestsPresenter{}

// toolRunTests implements the package test tool with optional lint checks.
type toolRunTests struct {
	sandboxAbsDir string                // This is the absolute sandbox root used to resolve requested paths.
	authorizer    authdomain.Authorizer // This authorizes reads before tests and test-time lints run.
	lintSteps     []lints.Step          // This is the configured lint pipeline used after tests run.
}

// runTestsParams contains the JSON parameters for the package test tool.
type runTestsParams struct {
	Path     string `json:"path"`      // This is the package path to test.
	TestName string `json:"test_name"` // This optionally selects tests to run with go test -run.
	Verbose  bool   `json:"verbose"`   // This enables verbose go test output when true.
	Env      string `json:"env"`       // This optionally supplies environment variables for go test.
}

// NewRunTestsTool returns a tool that runs tests for a package path. The tool resolves requested paths from authorizer's sandbox, uses authorizer to authorize reads,
// and uses the provided lint steps for optional lint checks after tests. authorizer must be non-nil.
func NewRunTestsTool(authorizer authdomain.Authorizer, lintSteps []lints.Step) llmstream.Tool {
	sandboxAbsDir := authorizer.SandboxDir()
	return &toolRunTests{
		sandboxAbsDir: sandboxAbsDir,
		authorizer:    authorizer,
		lintSteps:     lintSteps,
	}
}

// Name returns ToolNameRunTests.
func (t *toolRunTests) Name() string {
	return ToolNameRunTests
}

// Presenter returns the test presentation formatter.
func (t *toolRunTests) Presenter() llmstream.Presenter {
	return runTestsPresenterInstance
}

// runTestsPresenter formats package test tool calls and results for display.
type runTestsPresenter struct{}

// Present returns the display presentation for a package test tool call or result.
func (p runTestsPresenter) Present(call llmstream.ToolCall, result *llmstream.ToolResult) llmstream.Presentation {
	action := "Run Tests"
	if result != nil {
		action = "Ran Tests"
	}

	presentation := extToolSummaryPresentation(action, runTestsPresenterTarget(call))
	if result == nil {
		return presentation
	}

	content, payload, ok := extToolResultPayloadContent(*result)
	if !ok {
		return presentation
	}

	content = strings.ReplaceAll(strings.TrimSpace(content), "\r\n", "\n")
	if summary, ok := summarizeRunTestsSections(content); ok {
		presentation.Status = runTestsPresenterStatus(*result, payload, summary)
		presentation.Body = llmstream.Paragraph{
			Lines: []llmstream.Line{{
				Segments: []llmstream.Segment{
					{Text: summary.line, Role: llmstream.RoleAccent},
				},
			}},
		}
		return presentation
	}

	content = stripOuterXMLTag(content)
	if output, ok := summarizePresenterOutput(content, 5); ok {
		presentation.Body = output
	}
	return presentation
}

// Info returns the test tool metadata and parameter schema.
func (t *toolRunTests) Info() llmstream.ToolInfo {
	return llmstream.ToolInfo{
		Name:        ToolNameRunTests,
		Description: strings.TrimSpace(descriptionRunTests),
		Parameters: map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Filesystem path to the Go package to test (absolute, or relative to the sandbox directory)",
			},
			"test_name": map[string]any{
				"type":        "string",
				"description": "Optional test name to pass via go test -run",
			},
			"verbose": map[string]any{
				"type":        "boolean",
				"description": "Optional flag to run go test with -v",
			},
			"env": map[string]any{
				"type":        "string",
				"description": "Optional env vars for go test (ex: `MYVAR=1 OTHERVAR=2`)",
			},
		},
		Required: []string{"path"},
	}
}

// Run executes tests and test-time lints for the requested package path.
func (t *toolRunTests) Run(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
	var params runTestsParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return coretools.NewToolErrorResult(call, fmt.Sprintf("error parsing parameters: %s", err), err)
	}

	if params.Path == "" {
		return llmstream.NewErrorToolResult("path is required", call)
	}

	absPkgPath, _, normErr := coretools.NormalizePath(params.Path, t.sandboxAbsDir, coretools.WantPathTypeDir, true)
	if normErr != nil {
		return coretools.NewToolErrorResult(call, normErr.Error(), normErr)
	}

	if t.authorizer != nil {
		if authErr := t.authorizer.IsAuthorizedForRead(false, "", ToolNameRunTests, absPkgPath); authErr != nil {
			return coretools.NewToolErrorResult(call, authErr.Error(), authErr)
		}
	}

	output, err := RunTests(ctx, t.sandboxAbsDir, absPkgPath, params.TestName, params.Verbose, params.Env)
	if err != nil {
		return coretools.NewToolErrorResult(call, fmt.Sprintf("failed to run go test: %v", err), err)
	}
	lintOutput, err := runLints(ctx, t.sandboxAbsDir, absPkgPath, t.lintSteps, lints.SituationTests)
	if err != nil {
		return coretools.NewToolErrorResult(call, fmt.Sprintf("failed to run lints: %v", err), err)
	}
	if strings.HasSuffix(output, "\n") {
		output += lintOutput
	} else {
		output += "\n" + lintOutput
	}

	return llmstream.ToolResult{
		CallID: call.CallID,
		Name:   call.Name,
		Type:   call.Type,
		Result: output,
	}
}

// RunTests returns the output of the `go test` command, optionally verbose, optionally matched with namePattern, and optionally with env var assignments in env.
// ctx controls command cancellation; if nil, context.Background is used. The result is wrapped in a <test-status> XML tag:
//
//	<test-status ok="true">
//	$ go test -run TestMyTest ./codeai/tools
//	ok
//	</test-status>
//
// An error is only returned if the inputs are invalid (ex: pkgDirPath can't be found).
func RunTests(ctx context.Context, sandboxDir string, pkgDirPath string, namePattern string, verbose bool, env string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	envAssignments, err := parseEnvAssignments(env)
	if err != nil {
		return "", err
	}

	runner := newGoTestRunner(envAssignments)
	result, err := runner.Run(ctx, sandboxDir, map[string]any{
		"path":        pkgDirPath,
		"namePattern": namePattern,
		"verbose":     verbose,
		"Lang":        "go",
	})
	if err != nil {
		return "", err
	}

	return result.ToXML("test-status"), nil
}

// newGoTestRunner constructs a command runner for a single go test invocation. The runner requires path, accepts namePattern and verbose inputs, runs from the manifest
// directory for the requested path, and applies envAssignments to the command environment.
func newGoTestRunner(envAssignments []string) *cmdrunner.Runner {
	inputSchema := map[string]cmdrunner.InputType{
		"path":        cmdrunner.InputTypePathDir,
		"namePattern": cmdrunner.InputTypeString,
		"verbose":     cmdrunner.InputTypeBool,
		"Lang":        cmdrunner.InputTypeString,
	}
	runner := cmdrunner.NewRunner(inputSchema, []string{"path"})
	testArgs := []string{
		"{{ if .verbose }}-v{{ end }}",
		"{{ if ne .namePattern \"\" }}-run{{ end }}",
		"{{ if ne .namePattern \"\" }}{{ .namePattern }}{{ end }}",
		"{{ if eq .path (manifestDir .path) }}.{{ else }}./{{ relativeTo .path (manifestDir .path) }}{{ end }}",
	}
	runner.AddCommand(cmdrunner.Command{
		Command: "go",
		Args: append([]string{
			"test",
		}, testArgs...),
		CWD: "{{ manifestDir .path }}",
		Env: append([]string(nil), envAssignments...),
	})
	return runner
}
func parseEnvAssignments(env string) ([]string, error) {
	if env == "" {
		return nil, nil
	}
	assignments := strings.Fields(env)
	for _, assignment := range assignments {
		key, _, found := strings.Cut(assignment, "=")
		if !found || key == "" {
			return nil, fmt.Errorf("invalid env assignment %q: expected KEY=VALUE", assignment)
		}
		if !isValidEnvKey(key) {
			return nil, fmt.Errorf("invalid env key %q", key)
		}
	}
	return assignments, nil
}
func isValidEnvKey(key string) bool {
	for i := 0; i < len(key); i++ {
		ch := key[i]
		if i == 0 {
			if ch == '_' || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') {
				continue
			}
			return false
		}
		if ch == '_' || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') {
			continue
		}
		return false
	}
	return true
}

func runTestsPresenterTarget(call llmstream.ToolCall) string {
	var params runTestsParams
	if err := json.Unmarshal([]byte(call.Input), &params); err == nil {
		if path := strings.TrimSpace(params.Path); path != "" {
			return path
		}
	}
	if name := strings.TrimSpace(call.Name); name != "" {
		return name
	}
	return ToolNameRunTests
}

// runTestsXMLSection describes a discovered XML-like test or lint status section.
type runTestsXMLSection struct {
	found   bool // This reports whether the section tag was present.
	okFound bool // This reports whether the section tag included a parseable ok attribute.
	ok      bool // This is the parsed ok attribute value when okFound is true.
}

// runTestsSectionsSummary summarizes discovered test and lint status sections.
type runTestsSectionsSummary struct {
	tests runTestsXMLSection // This is the discovered test-status section.
	lints runTestsXMLSection // This is the discovered lint-status section.
	line  string             // This is the concise display line for the presentation body.
}

// extractRunTestsXMLSection extracts the presence and ok status of a named XML-like section.
func extractRunTestsXMLSection(content string, tagName string) runTestsXMLSection {
	needle := "<" + tagName
	openStart := strings.Index(content, needle)
	if openStart < 0 {
		return runTestsXMLSection{}
	}

	gtRel := strings.IndexByte(content[openStart:], '>')
	if gtRel < 0 {
		return runTestsXMLSection{}
	}

	openTag := content[openStart : openStart+gtRel+1]
	section := runTestsXMLSection{found: true}
	if ok, found := extractXMLishOK(openTag); found {
		section.ok = ok
		section.okFound = true
	}
	return section
}

func runTestsStatusWord(section runTestsXMLSection) string {
	if !section.okFound {
		return "unknown"
	}
	if section.ok {
		return "pass"
	}
	return "fail"
}

// summarizeRunTestsSections extracts test and lint status sections into a concise presentation summary.
func summarizeRunTestsSections(content string) (runTestsSectionsSummary, bool) {
	content = strings.TrimSpace(content)
	if content == "" {
		return runTestsSectionsSummary{}, false
	}

	summary := runTestsSectionsSummary{
		tests: extractRunTestsXMLSection(content, "test-status"),
		lints: extractRunTestsXMLSection(content, "lint-status"),
	}
	if !summary.tests.found && !summary.lints.found {
		return runTestsSectionsSummary{}, false
	}

	testsWord := "-"
	if summary.tests.found {
		testsWord = runTestsStatusWord(summary.tests)
	}
	lintsWord := "-"
	if summary.lints.found {
		lintsWord = runTestsStatusWord(summary.lints)
	}
	summary.line = "Tests: " + testsWord + " | Lints: " + lintsWord
	return summary, true
}

// runTestsPresenterStatus returns the explicit presentation status for a run_tests result. Payload success takes precedence, followed by parsed test-status and
// lint-status ok attributes, and finally result.IsError. When no failure signal is present, it returns PresentationStatusSuccess.
func runTestsPresenterStatus(result llmstream.ToolResult, payload extToolPayload, summary runTestsSectionsSummary) llmstream.PresentationStatus {
	if payload.Success != nil {
		if *payload.Success {
			return llmstream.PresentationStatusSuccess
		}
		return llmstream.PresentationStatusFailure
	}

	if summary.tests.found && summary.lints.found && summary.tests.okFound && summary.lints.okFound {
		if summary.tests.ok && summary.lints.ok {
			return llmstream.PresentationStatusSuccess
		}
		return llmstream.PresentationStatusFailure
	}
	if summary.tests.found && summary.tests.okFound {
		if summary.tests.ok {
			return llmstream.PresentationStatusSuccess
		}
		return llmstream.PresentationStatusFailure
	}
	if summary.lints.found && summary.lints.okFound {
		if summary.lints.ok {
			return llmstream.PresentationStatusSuccess
		}
		return llmstream.PresentationStatusFailure
	}
	if result.IsError {
		return llmstream.PresentationStatusFailure
	}
	return llmstream.PresentationStatusSuccess
}
