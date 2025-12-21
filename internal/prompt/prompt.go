package prompt

import (
	"bytes"
	_ "embed"
	"strings"
	"text/template"

	"github.com/codalotl/codalotl/internal/llmmodel"
)

// Thought: i kinda want to make a database of atomic "ideas". Most a single short sentence. Then I can test full prompts against the ideas, or generate new prompts given a set of target ideas.
// Example ideas:
//   - "wrap file references in backticks"
//   - "communicate concisely"
//   - "don't stop until you've solved the user's request"

var (
	// //go:embed fragments/header.md
	// genericHeader string

	//go:embed fragments/header-codex.md
	codexHeader string

	//go:embed fragments/sub-header.md
	capabilitiesSection string

	//go:embed fragments/personality.md
	personalitySection string

	//go:embed fragments/code-editing.md
	codeEditingSection string

	//go:embed fragments/sandbox-approvals-safety.md
	sandboxApprovalsSafetySection string

	//go:embed fragments/tools.md
	toolsSection string

	//go:embed fragments/planning.md
	planningSection string

	//go:embed fragments/git-and-version-control.md
	gitAndVersionControlSection string

	//go:embed fragments/final-message.md
	finalMessageSection string

	//go:embed fragments/message-formatting.md
	messageFormattingSection string

	//go:embed fragments/go-package-mode.md
	goPackageModeSection string
)

// GetFullPrompt returns a prompt for modelID, where the agent is named agentName.
// Different models have are best prompted in different ways, often based on how they were RL'ed. This method
// returns a prompt well-suited for that model.
func GetFullPrompt(agentName string, modelID llmmodel.ModelID) string {
	data := map[string]any{
		"AgentName": agentName,
		"ModelName": modelDisplayName(modelID),
	}

	// NOTE: in future could do stuff based on modelID.Provider(), eg, render anthropic-specific prompts.

	sections := []string{
		codexHeader,
		capabilitiesSection,
		personalitySection,
		codeEditingSection,
		sandboxApprovalsSafetySection,
		toolsSection,
		planningSection,
		gitAndVersionControlSection,
		finalMessageSection,
		messageFormattingSection,
	}
	rendered := make([]string, 0, len(sections))
	for _, fragment := range sections {
		fragment = strings.TrimSpace(fragment)
		rendered = append(rendered, renderFragment(fragment, data))
	}

	return strings.Join(rendered, "\n\n")
}

// GetGoPackageModeModePrompt returns a system prompt for agentName/modelID that extends GetFullPrompt with an explanation of the full/default package mode.
//
// Any limited package-mode (ex: update_usage, clarify_docs) may be better off using GetFullPrompt and then adding their own limited package-mode explanation.
func GetGoPackageModeModePrompt(agentName string, modelID llmmodel.ModelID) string {
	data := map[string]any{
		"AgentName": agentName,
		"ModelName": modelDisplayName(modelID),
	}

	sections := []string{
		codexHeader,
		capabilitiesSection,
		personalitySection,
		codeEditingSection,
		sandboxApprovalsSafetySection,
		toolsSection,
		planningSection,
		gitAndVersionControlSection,
		finalMessageSection,
		messageFormattingSection,
		goPackageModeSection,
	}
	rendered := make([]string, 0, len(sections))
	for _, fragment := range sections {
		fragment = strings.TrimSpace(fragment)
		rendered = append(rendered, renderFragment(fragment, data))
	}

	return strings.Join(rendered, "\n\n")
}

func renderFragment(fragment string, data any) string {
	tmpl, err := template.New("fragment").Option("missingkey=zero").Parse(fragment)
	if err != nil {
		return fragment
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fragment
	}
	return buf.String()
}

func modelDisplayName(modelID llmmodel.ModelID) string {
	name := strings.TrimSpace(string(modelID))
	if name == "" {
		return "an unspecified model"
	}
	return name
}
