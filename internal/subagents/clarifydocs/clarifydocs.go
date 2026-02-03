package clarifydocs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/detectlang"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/initialcontext"
	"github.com/codalotl/codalotl/internal/prompt"
	"github.com/codalotl/codalotl/internal/q/cmdrunner"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/codalotl/codalotl/internal/tools/coretools"
	"github.com/codalotl/codalotl/internal/tools/toolsetinterface"
)

const (
	rgContextLines = "4"
)

// ClarifyAPI clarifies the API/docs for identifier found in path and returns an answer. An error is returned for invalid inputs, failure to communicate with the LLM, etc.
// If the LLM can't find the identifier as it relates to path, it may say so in the answer, which doesn't produce an error.
//   - sandboxAbsDir is used for tool construction and relative path resolution, not as a confinement mechanism.
//   - authorizer is optional. If present, it confines the SubAgent in some way (usually to a sandbox dir of some kind).
//   - toolset toolsetinterface.Toolset are the tools available for use. Injected to cut dependencies. Should be ls/read_file.
//   - path is absolute or relative to sandboxAbsDir. If absolute, it may be outside of sandboxAbsDir (for instance, when clarifying dep packages or stdlib packages).
//   - identifier is language-specific and opaque. For Go, it looks like "MyVar", "*MyType.MyFunc", etc.
//
// When clarifying a dep package outside of the sandbox, and authorizer is not nil, it is recommended for UX reasons (but not required) to construct an authorizer to allow reads. There are many
// ways to do this - one is to create a new authorizer with sandbox root of the dep; another is to add a 'grant' to the authorizer.
//
// Example question: "What does the first return parameter (a string) look like in the ClarifyAPI func?". Example answer that might be returned: "The ClarifyAPI func
// returns a human- or LLM-readable answer to the specified question. It will be the empty string if an error occurred."
func ClarifyAPI(ctx context.Context, agentCreator agent.AgentCreator, sandboxAbsDir string, authorizer authdomain.Authorizer, toolset toolsetinterface.Toolset, path string, identifier string, question string) (string, error) {
	if agentCreator == nil {
		return "", errors.New("agentCreator is required")
	}
	path = strings.TrimSpace(path)
	identifier = strings.TrimSpace(identifier)
	question = strings.TrimSpace(question)
	if path == "" {
		return "", errors.New("path is required")
	}
	if identifier == "" {
		return "", errors.New("identifier is required")
	}
	if question == "" {
		return "", errors.New("question is required")
	}

	var absPath string
	if filepath.IsAbs(path) {
		var err error
		absPath, err = filepath.Abs(path)
		if err != nil {
			return "", fmt.Errorf("make path absolute: %w", err)
		}
	} else {
		var err error
		absPath, _, err = coretools.NormalizePath(path, sandboxAbsDir, coretools.WantPathTypeAny, true)
		if err != nil {
			return "", fmt.Errorf("normalize path: %w", err)
		}
		if absPath == "" {
			return "", fmt.Errorf("path %q is outside of sandbox %q", path, sandboxAbsDir)
		}
	}

	stat, err := os.Stat(absPath)
	if err != nil {
		return "", fmt.Errorf("stat path %q: %w", absPath, err)
	}

	if err := ctx.Err(); err != nil {
		return "", err
	}

	lang, _ := detectlang.Detect(sandboxAbsDir, absPath) // Ignore error

	var contextStr string
	useGeneric := true
	if lang == detectlang.LangGo {
		goContext, goDetected, err := tryBuildGoContext(sandboxAbsDir, absPath)
		if err != nil {
			return "", err
		}
		if goDetected {
			contextStr = goContext
			useGeneric = false
		}
	}

	if useGeneric {
		genericContext, err := buildGenericContext(absPath, stat, identifier)
		if err != nil {
			return "", err
		}
		contextStr = genericContext
	}

	tools, err := toolset(sandboxAbsDir, authorizer)
	if err != nil {
		return "", err
	}

	systemPrompt := prompt.GetFullPrompt()

	ag, err := agentCreator.NewWithDefaultModel(systemPrompt, tools)
	if err != nil {
		return "", fmt.Errorf("create agent: %w", err)
	}

	message := buildPrompt(absPath, identifier, question, contextStr)

	events := ag.SendUserMessage(ctx, message)

	var finalTurnText string

	for event := range events {
		switch event.Type {
		case agent.EventTypeError:
			if event.Error != nil {
				return "", fmt.Errorf("agent error: %w", event.Error)
			}
			return "", errors.New("agent error: unspecified")
		case agent.EventTypeDoneSuccess, agent.EventTypeAssistantTurnComplete:
			if event.Turn != nil {
				finalTurnText = event.Turn.TextContent()
			}
		case agent.EventTypeCanceled:
			return "", errors.New("agent conversation canceled")
		}
	}

	answer := strings.TrimSpace(finalTurnText)

	return answer, nil
}

func buildPrompt(absPath, identifier, question, initialContext string) string {
	var buf strings.Builder
	buf.WriteString("Your task is to answer the user's question about the specified identifier using the provided context and available tools (`ls`, `read_file`).\n")
	buf.WriteString("If information is missing or the identifier cannot be found, clearly state that and explain what would be needed.\n\n")

	buf.WriteString("Identifier: ")
	buf.WriteString(identifier)
	buf.WriteRune('\n')

	buf.WriteString("Path: ")
	buf.WriteString(absPath)
	buf.WriteRune('\n')
	buf.WriteRune('\n')

	if initialContext != "" {
		buf.WriteString(initialContext)
	}
	buf.WriteString("\n\n")

	buf.WriteString("Question:\n")
	buf.WriteString(question)
	buf.WriteString("\n\nPlease respond with a concise, well-structured clarification that directly addresses the question. Keep in mind the questioner cannot see function bodies")
	buf.WriteString("or other non-exported code within the indicated path, and is relying entirely on your description.")

	return buf.String()
}

func tryBuildGoContext(sandboxAbsDir, absPath string) (string, bool, error) {
	module, err := gocode.NewModule(absPath)
	if err != nil {
		return "", false, nil
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return "", false, fmt.Errorf("stat path for Go context: %w", err)
	}

	pkgDir := absPath
	if !info.IsDir() {
		pkgDir = filepath.Dir(absPath)
	}

	relDir, err := filepath.Rel(module.AbsolutePath, pkgDir)
	if err != nil {
		return "", false, fmt.Errorf("determine package relative dir: %w", err)
	}
	relDir = filepath.ToSlash(relDir)

	pkg, err := module.LoadPackageByRelativeDir(relDir)
	if err != nil {
		return "", false, nil
	}

	initial, err := initialcontext.Create(sandboxAbsDir, pkg)
	if err != nil {
		return "", false, fmt.Errorf("initial context: %w", err)
	}

	return initial, true, nil
}

func buildGenericContext(absPath string, stat os.FileInfo, identifier string) (string, error) {
	var dir string
	var target string
	if stat.IsDir() {
		dir = absPath
		target = "."
	} else {
		dir = filepath.Dir(absPath)
		target = filepath.Base(absPath)
	}

	rgOutput := runRipgrep(dir, target, identifier)
	if rgOutput == "" {
		return "", nil
	}

	return rgOutput, nil
}

func runRipgrep(cwd, target, identifier string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	runner := cmdrunner.NewRunner(nil, nil)

	// TODO: add option for cmdrunner.Command to limit output size
	runner.AddCommand(cmdrunner.Command{
		Command: "rg",
		Args: []string{
			"--line-number",
			"--color=never",
			"-C", rgContextLines,
			identifier,
			target,
		},
		CWD:     cwd,
		ShowCWD: true,
	})

	result, err := runner.Run(ctx, cwd, nil)
	if err != nil {
		return fmt.Sprintf("Failed to run ripgrep: %v", err)
	}

	xml := result.ToXML("ripgrep")

	return xml
}
