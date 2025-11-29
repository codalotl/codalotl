package updateusage

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
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/prompt"
	"github.com/codalotl/codalotl/internal/tools/auth"
	"github.com/codalotl/codalotl/internal/tools/toolsetinterface"
)

// UpdateUsage updates a package according to the given instructions. It returns the agent's last message. An error is returned for invalid inputs, failure to communicate with the LLM, etc.
// If the LLM can't find the make the updates as per instructions, it may say so in its answer, which doesn't produce an error.
//   - sandboxAbsDir is used for tool construction and relative path resolution, not as a confinement mechanism.
//   - authorizer is optional. If present, it confines the SubAgent in some way (usually to a sandbox dir of some kind).
//   - sandboxAuthorizer is optional. If present, it confines the SubAgent in some way (usually to a sandbox dir of some kind).
//   - goPkgAbsDir is the absolute path to a package.
//   - toolset toolsetinterface.Toolset are the tools available for use. Injected to cut dependencies. Should be ls/read_file.
//   - instructions must contain enough information for an LLM to update the package (it won't have the context of the calling agent). The instructions should often have **selection** instructions:
//     update this package IF it uses Xyz function.
//
// Example instructions: "Update the package to use testify."
func UpdateUsage(ctx context.Context, agentCreator agent.AgentCreator, sandboxAbsDir string, authorizer auth.Authorizer, sandboxAuthorizer auth.Authorizer, goPkgAbsDir string, toolset toolsetinterface.PackageToolset, instructions string) (string, error) {
	if agentCreator == nil {
		return "", errors.New("agentCreator is required")
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

	lang, _ := detectlang.Detect(sandboxAbsDir, goPkgAbsDir) // Ignore error

	var contextStr string

	switch lang {
	case detectlang.LangGo:
		goContext, err := buildGoContext(sandboxAbsDir, goPkgAbsDir)
		if err != nil {
			return "", err
		}
		contextStr = goContext
	default:
		return "", fmt.Errorf("only go is supported right now")
	}

	tools, err := toolset(sandboxAbsDir, authorizer, sandboxAuthorizer, goPkgAbsDir)
	if err != nil {
		return "", err
	}

	systemPrompt := prompt.GetFullPrompt("Updater", llmmodel.ModelIDUnknown) // TODO: fix this

	ag, err := agentCreator.NewWithDefaultModel(systemPrompt, tools)
	if err != nil {
		return "", fmt.Errorf("create agent: %w", err)
	}

	message := buildPrompt(goPkgAbsDir, instructions, contextStr)
	// fmt.Println("---Message---")
	// fmt.Println(message)
	// fmt.Println("---^^------")

	// if message != "" {
	// 	return "", nil
	// }

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

func buildPrompt(goPkgAbsDir, instructions, initialContext string) string {
	var buf strings.Builder
	buf.WriteString("You are a small agent designed to make targeted, mechanical changes to a single package.\n")
	buf.WriteString("Your task is to make the indicated changes as per the instructions below.\n\n")

	buf.WriteString(initialContext)
	buf.WriteString("\n\n")

	buf.WriteString("Instructions:\n")
	buf.WriteString(instructions)
	buf.WriteString("\n\nRespond with a concise, well-structured summary of the changes you made, as well as the outcome of whether the changes were successful.\n")
	buf.WriteString("If the changes couldn't be made, concisely state the reasons why. This might occur if:\n")
	buf.WriteString("- The instructions are unclear or ambiguous.\n")
	buf.WriteString("- Following the instructions would require propagating more changes to other packages.\n")
	buf.WriteString("- After making the changes, tests don't pass, indicating a problem upstream.\n")
	buf.WriteString("\n")

	return buf.String()
}

func buildGoContext(sandboxAbsDir, goPkgAbsDir string) (string, error) {
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

	initial, err := initialcontext.Create(sandboxAbsDir, pkg)
	if err != nil {
		return "", fmt.Errorf("initial context: %w", err)
	}

	return initial, nil
}
