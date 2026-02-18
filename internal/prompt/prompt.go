package prompt

import (
	"bytes"
	_ "embed"
	"strings"
	"sync"
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

	//go:embed fragments/go-package-mode-update_usage.md
	goPackageModeUpdateUsageSection string
)

var (
	cfgMu        sync.RWMutex
	cfgAgentName = "Codalotl"
	cfgModelID   = llmmodel.DefaultModel
)

// SetAgentName sets the globally configured agent name used when rendering prompts.
//
// This is expected to be called once by internal/cli during startup.
func SetAgentName(agentName string) {
	cfgMu.Lock()
	cfgAgentName = strings.TrimSpace(agentName)
	cfgMu.Unlock()
}

// SetModel sets the globally configured model used when rendering prompts.
//
// This is expected to be called once by internal/cli during startup.
func SetModel(modelID llmmodel.ModelID) {
	cfgMu.Lock()
	cfgModelID = modelID
	cfgMu.Unlock()
}

func getConfig() (agentName string, modelID llmmodel.ModelID) {
	cfgMu.RLock()
	agentName = cfgAgentName
	modelID = cfgModelID
	cfgMu.RUnlock()

	agentName = strings.TrimSpace(agentName)
	if agentName == "" {
		agentName = "Codalotl"
	}
	if strings.TrimSpace(string(modelID)) == "" {
		modelID = llmmodel.DefaultModel
	}
	return agentName, modelID
}

// GetFullPrompt returns a prompt using the globally configured agent name and model. Different models have are best prompted in different ways, often based on how
// they were RL'ed. This method returns a prompt well-suited for that model.
func GetFullPrompt() string {
	agentName, modelID := getConfig()
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

type GoPackageModePromptKind string

const (
	GoPackageModePromptKindFull        GoPackageModePromptKind = "full"
	GoPackageModePromptKindUpdateUsage GoPackageModePromptKind = "update_usage"
)

// GetGoPackageModeModePrompt returns a system prompt using the globally configured agent name and model that extends GetFullPrompt with a package mode of the requested
// kind (GoPackageModePromptKindFull is the full, default package mode).
//
// To make subagents with a subset of tools/capabilities, add a GoPackageModePromptKind with a custom explanation.
func GetGoPackageModeModePrompt(kind GoPackageModePromptKind) string {
	agentName, modelID := getConfig()
	base := GetFullPrompt()

	data := map[string]any{
		"AgentName": agentName,
		"ModelName": modelDisplayName(modelID),
	}

	var snippet string

	switch kind {
	case GoPackageModePromptKindFull:
		snippet = goPackageModeSection
	case GoPackageModePromptKindUpdateUsage:
		snippet = goPackageModeUpdateUsageSection
	default:
		panic("unhandled package mode kind")
	}

	return base + "\n\n" + renderFragment(strings.TrimSpace(snippet), data)
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
