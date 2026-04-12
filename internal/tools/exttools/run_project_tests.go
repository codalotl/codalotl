package exttools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/q/cmdrunner"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/codalotl/codalotl/internal/tools/coretools"
	"strings"
)

//go:embed run_project_tests.md
var descriptionRunProjectTests string

const ToolNameRunProjectTests = "run_project_tests"

var runProjectTestsPresenterInstance llmstream.Presenter = runProjectTestsPresenter{}

type toolRunProjectTests struct {
	sandboxAbsDir string
	pkgDirAbsPath string
	authorizer    authdomain.Authorizer
}

// type runProjectTestsParams struct{}

// NewRunProjectTestsTool runs `go test ./...`. if pkgDirAbsPath is set, we run the project tests with respect to that dir's containing Go module. Otherwise, run
// the command in sandboxAbsDir hope for the best.
func NewRunProjectTestsTool(pkgDirAbsPath string, authorizer authdomain.Authorizer) llmstream.Tool {
	sandboxAbsDir := authorizer.SandboxDir()
	return &toolRunProjectTests{
		sandboxAbsDir: sandboxAbsDir,
		pkgDirAbsPath: pkgDirAbsPath,
		authorizer:    authorizer,
	}
}

func (t *toolRunProjectTests) Name() string {
	return ToolNameRunProjectTests
}

func (t *toolRunProjectTests) Presenter() llmstream.Presenter {
	return runProjectTestsPresenterInstance
}

type runProjectTestsPresenter struct{}

func (p runProjectTestsPresenter) Present(call llmstream.ToolCall, result *llmstream.ToolResult) llmstream.Presentation {
	action := "Run Tests"
	if result != nil {
		action = "Ran Tests"
	}

	presentation := extToolSummaryPresentation(action, "./...")
	if result == nil {
		return presentation
	}

	success, content := extractRunProjectTestsContent(*result)
	if success {
		presentation.Body = llmstream.Paragraph{
			Lines: []llmstream.Line{{
				Segments: []llmstream.Segment{
					{Text: "Passed", Role: llmstream.RoleAccent},
				},
			}},
		}
		return presentation
	}

	presentation.ErrorBehavior = llmstream.ErrorBehaviorPresenterOwned
	lines := []llmstream.Line{{
		Segments: []llmstream.Segment{
			{Text: "Failed:", Role: llmstream.RoleAccent},
		},
	}}
	for _, failure := range extractRunProjectTestsFailures(content) {
		lines = append(lines, llmstream.Line{
			Segments: []llmstream.Segment{
				{Text: failure, Role: llmstream.RoleAccent},
			},
		})
	}
	presentation.Body = llmstream.Paragraph{Lines: lines}
	return presentation
}

func (t *toolRunProjectTests) Info() llmstream.ToolInfo {
	return llmstream.ToolInfo{
		Name:        ToolNameRunProjectTests,
		Description: strings.TrimSpace(descriptionRunProjectTests),
		Parameters:  map[string]any{},
	}
}

func (t *toolRunProjectTests) Run(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
	basePath := t.pkgDirAbsPath

	absPath, _, normErr := coretools.NormalizePath(basePath, t.sandboxAbsDir, coretools.WantPathTypeDir, true)
	if normErr != nil {
		return coretools.NewToolErrorResult(call, normErr.Error(), normErr)
	}
	runner := newGoProjectTestRunner()
	result, err := runner.Run(ctx, t.sandboxAbsDir, map[string]any{
		"path": absPath,
		"Lang": "go",
	})
	if err != nil {
		return coretools.NewToolErrorResult(call, fmt.Sprintf("failed to run go test ./...: %v", err), err)
	}

	return llmstream.ToolResult{
		CallID: call.CallID,
		Name:   call.Name,
		Type:   call.Type,
		Result: result.ToXML("test-status"),
	}
}

func newGoProjectTestRunner() *cmdrunner.Runner {
	inputSchema := map[string]cmdrunner.InputType{
		"path": cmdrunner.InputTypePathDir,
		"Lang": cmdrunner.InputTypeString,
	}
	r := cmdrunner.NewRunner(inputSchema, []string{"path"})
	r.AddCommand(cmdrunner.Command{
		Command: "go",
		Args: []string{
			"test",
			"./...",
		},
		CWD: "{{ manifestDir .path }}",
	})
	return r
}

func extractRunProjectTestsContent(result llmstream.ToolResult) (bool, string) {
	success := true
	trimmed := strings.TrimSpace(result.Result)
	if trimmed == "" {
		if explicit, ok := extToolResultSuccess(result); ok {
			return explicit, ""
		}
		return !result.IsError, ""
	}

	var payload extToolPayload
	if err := json.Unmarshal([]byte(trimmed), &payload); err == nil {
		content := payload.Content
		if strings.TrimSpace(content) == "" && strings.TrimSpace(payload.Error) != "" {
			content = payload.Error
		}
		if payload.Success != nil {
			success = *payload.Success
		} else if explicit, ok := extToolResultSuccess(result); ok {
			success = explicit
		} else {
			success = !result.IsError
		}
		return success, content
	}

	if explicit, ok := extToolResultSuccess(result); ok {
		success = explicit
	} else {
		success = !result.IsError
	}
	return success, trimmed
}

func extractRunProjectTestsFailures(content string) []string {
	content = stripOuterXMLTag(strings.TrimSpace(content))
	content = strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(content, "\n")

	failures := make([]string, 0, len(lines))
	seen := map[string]struct{}{}
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		failures = append(failures, s)
	}

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "Failed:") {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "Failed:"))
		if rest != "" {
			add(rest)
		}
		for _, next := range lines[i+1:] {
			candidate := strings.TrimSpace(next)
			if candidate == "" {
				continue
			}
			if strings.HasPrefix(candidate, "</") && strings.HasSuffix(candidate, ">") {
				break
			}
			add(candidate)
		}
		return failures
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "FAIL\t") && !strings.HasPrefix(trimmed, "FAIL ") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 2 {
			continue
		}
		add(fields[1])
	}

	return failures
}
