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
	"github.com/codalotl/codalotl/internal/lints"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/prompt"
	"github.com/codalotl/codalotl/internal/skills"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/codalotl/codalotl/internal/tools/toolsetinterface"
)

// Run runs an agent with the given instructions and tools on a specific package.
//
// It returns the agent's last message. An error is returned for invalid inputs, failure to communicate with the LLM, etc. If the LLM can't make the updates as per
// instructions, it may say so in its answer, which doesn't produce an error.
//
//   - authorizer is a code unit authorizer.
//   - goPkgAbsDir is the absolute path to a package.
//   - toolset are the tools available for use (injected to cut dependencies).
//   - instructions must contain enough information for an LLM to update the package (it won't have the context of the calling agent).
//   - lintSteps controls lint checks in initial context collection and lint-aware tools.
//
// Example instructions: "Update the package add a IsDefault field to the Configuration struct."
func Run(ctx context.Context, agentCreator agent.AgentCreator, authorizer authdomain.Authorizer, goPkgAbsDir string, toolset toolsetinterface.Toolset, instructions string, lintSteps []lints.Step, promptKind prompt.GoPackageModePromptKind) (string, error) {
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
		goContext, err := buildGoContext(goPkgAbsDir, lintSteps)
		if err != nil {
			return "", err
		}
		contextStr = goContext
	default:
		return "", fmt.Errorf("only go is supported right now")
	}

	tools, err := toolset(toolsetinterface.Options{
		SandboxDir:  sandboxAbsDir,
		Authorizer:  authorizer,
		GoPkgAbsDir: goPkgAbsDir,
		LintSteps:   lintSteps,
	})
	if err != nil {
		return "", err
	}

	systemPrompt, err := buildSystemPrompt(sandboxAbsDir, goPkgAbsDir, authorizer, tools, promptKind)
	if err != nil {
		return "", err
	}

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

func buildSystemPrompt(sandboxAbsDir string, goPkgAbsDir string, authorizer authdomain.Authorizer, tools []llmstream.Tool, promptKind prompt.GoPackageModePromptKind) (string, error) {
	// Provide the sandbox location in the system prompt so the model can reason about paths
	// and what it can/can't read via tools.
	systemPrompt := prompt.GetGoPackageModeModePrompt(promptKind) + "\n\n<env>\nSandbox directory: " + sandboxAbsDir + "\n</env>\n"

	searchDirs := skills.SearchPaths(goPkgAbsDir)
	validSkills, invalidSkills, failedSkillLoads, skillsErr := skills.LoadSkills(searchDirs)
	if skillsErr != nil {
		// Non-fatal: skills are optional; if discovery fails, just don't mention them.
		validSkills = nil
		invalidSkills = nil
		failedSkillLoads = nil
	}

	// Mirror internal/tui behavior: only mention/enable skills when there's a shell tool
	// exposed to the model for skill script execution.
	if shellToolName, ok := detectShellToolName(tools); ok {
		if err := skills.Authorize(validSkills, authorizer); err != nil {
			return "", fmt.Errorf("authorize skills: %w", err)
		}
		_ = invalidSkills
		_ = failedSkillLoads
		systemPrompt = joinContextBlocks(systemPrompt, skills.Prompt(validSkills, shellToolName, true))
	}

	return strings.TrimSpace(systemPrompt), nil
}

func buildGoContext(goPkgAbsDir string, lintSteps []lints.Step) (string, error) {
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

	initial, err := initialcontext.Create(pkg, lintSteps, false)
	if err != nil {
		return "", fmt.Errorf("initial context: %w", err)
	}

	return initial, nil
}

func detectShellToolName(tools []llmstream.Tool) (name string, ok bool) {
	// We want skills.Prompt to reference the actual shell tool name exposed to the LLM.
	// "skill_shell" is the harness-level default, but some toolsets may export it as "shell".
	for _, candidate := range []string{"skill_shell", "shell"} {
		for _, t := range tools {
			if t != nil && t.Name() == candidate {
				return candidate, true
			}
		}
	}
	return "", false
}

func joinContextBlocks(blocks ...string) string {
	nonEmpty := make([]string, 0, len(blocks))
	for _, b := range blocks {
		if strings.TrimSpace(b) == "" {
			continue
		}
		nonEmpty = append(nonEmpty, strings.TrimSpace(b))
	}
	return strings.Join(nonEmpty, "\n\n")
}
