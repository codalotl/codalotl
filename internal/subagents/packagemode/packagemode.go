package packagemode

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/detectlang"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/initialcontext"
	"github.com/codalotl/codalotl/internal/prompt"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/codalotl/codalotl/internal/tools/toolsetinterface"
)

// Run runs the agent with the given instructions and tools on a specific package. It returns the agent's last message. An error is returned for invalid inputs, failure to communicate with the LLM, etc.
// If the LLM can't find the make the updates as per instructions, it may say so in its answer, which doesn't produce an error.
//   - authorizer is a code unit authorizer.
//   - goPkgAbsDir is the absolute path to a package.
//   - toolset toolsetinterface.Toolset are the tools available for use. Injected to cut dependencies.
//   - instructions must contain enough information for an LLM to update the package (it won't have the context of the calling agent).
//
// Example instructions: "Update the package add a IsDefault field to the Configuration struct."
func Run(ctx context.Context, agentCreator agent.AgentCreator, authorizer authdomain.Authorizer, goPkgAbsDir string, toolset toolsetinterface.PackageToolset, instructions string, promptKind prompt.GoPackageModePromptKind) (string, error) {
	if agentCreator == nil {
		return "", errors.New("agentCreator is required")
	}
	if authorizer == nil {
		return "", errors.New("authorizer is required")
	}
	if toolset == nil {
		return "", errors.New("toolset is required")
	}
	if instructions == "" {
		return "", errors.New("instructions is required")
	}
	if !filepath.IsAbs(goPkgAbsDir) {
		return "", errors.New("goPkgAbsDir must be absolute")
	}
	stat, err := os.Stat(goPkgAbsDir)
	if err != nil {
		return "", fmt.Errorf("stat path %q: %w", goPkgAbsDir, err)
	}
	if !stat.IsDir() {
		return "", fmt.Errorf("goPkgAbsDir %q: must be dir", goPkgAbsDir)
	}

	if err := ctx.Err(); err != nil {
		return "", err
	}

	sandboxAbsDir := authorizer.SandboxDir()
	if sandboxAbsDir == "" || !filepath.IsAbs(sandboxAbsDir) {
		return "", fmt.Errorf("authorizer sandbox dir %q: must be absolute", sandboxAbsDir)
	}

	// Package-mode is only meaningful if tools are correctly jailed to the target package.
	// We enforce that by requiring an active code unit domain whose base dir is the package dir.
	if !authorizer.IsCodeUnitDomain() {
		return "", errors.New("authorizer must enforce a code-unit (package) domain")
	}
	if filepath.Clean(authorizer.CodeUnitDir()) != filepath.Clean(goPkgAbsDir) {
		return "", fmt.Errorf("authorizer code-unit dir %q must equal goPkgAbsDir %q", authorizer.CodeUnitDir(), goPkgAbsDir)
	}

	if promptKind == "" {
		promptKind = prompt.GoPackageModePromptKindFull
	}

	lang, _ := detectlang.Detect(sandboxAbsDir, goPkgAbsDir) // Ignore error

	var contextStr string
	switch lang {
	case detectlang.LangGo:
		goContext, err := buildGoContext(goPkgAbsDir)
		if err != nil {
			return "", err
		}
		contextStr = goContext
	default:
		return "", fmt.Errorf("only go is supported right now")
	}

	tools, err := toolset(sandboxAbsDir, authorizer, goPkgAbsDir)
	if err != nil {
		return "", err
	}

	// Provide the sandbox location in the system prompt so the model can reason about paths
	// and what it can/can't read via tools.
	systemPrompt := prompt.GetGoPackageModeModePrompt(promptKind) + "\n\n<env>\nSandbox directory: " + sandboxAbsDir + "\n</env>\n"

	ag, err := agentCreator.NewWithDefaultModel(systemPrompt, tools)
	if err != nil {
		return "", fmt.Errorf("create agent: %w", err)
	}

	// The system prompt encapsulates package-mode scope constraints. The user message
	// should just contain the initial context plus the task instructions.
	message := contextStr + "\n\nInstructions:\n" + instructions + "\n"

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

	return strings.TrimSpace(finalTurnText), nil
}

func buildGoContext(goPkgAbsDir string) (string, error) {
	module, err := gocode.NewModule(goPkgAbsDir)
	if err != nil {
		return "", err
	}

	relDir, err := filepath.Rel(module.AbsolutePath, goPkgAbsDir)
	if err != nil {
		return "", fmt.Errorf("determine package relative dir: %w", err)
	}
	relDir = filepath.ToSlash(relDir)

	pkg, err := module.LoadPackageByRelativeDir(relDir)
	if err != nil {
		return "", err
	}

	initial, err := initialcontext.Create(pkg, false)
	if err != nil {
		return "", fmt.Errorf("initial context: %w", err)
	}

	return initial, nil
}
