package prompt

import (
	"bytes"
	_ "embed"
	"strings"
	"sync"
	"text/template"

	"github.com/codalotl/codalotl/internal/llmmodel"
)

var (
	//go:embed data/basic.default.md
	basicDefault string

	//go:embed data/package_mode.default.md
	packageModeDefault string

	//go:embed data/package_mode_update_usage.default.md
	packageModeUpdateUsageDefault string
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

// GetBasicPrompt returns a prompt using the globally configured agent name and model. Different models have are best prompted in different ways, often based on
// how they were RL'ed. This method returns a prompt well-suited for that model.
func GetBasicPrompt() string {
	agentName, modelID := getConfig()
	data := promptTemplateData(agentName, modelID)

	// NOTE: in future could do stuff based on modelID.Provider(), eg, render anthropic-specific prompts.
	return renderFragment(strings.TrimSpace(basicDefault), data)
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
	data := promptTemplateData(agentName, modelID)
	base := renderFragment(strings.TrimSpace(basicDefault), data)

	var snippet string

	switch kind {
	case GoPackageModePromptKindFull:
		snippet = packageModeDefault
	case GoPackageModePromptKindUpdateUsage:
		snippet = packageModeUpdateUsageDefault
	default:
		panic("unhandled package mode kind")
	}

	return base + "\n\n" + renderFragment(strings.TrimSpace(snippet), data)
}

func promptTemplateData(agentName string, modelID llmmodel.ModelID) map[string]any {
	tools := decideFileEditTools(modelID)
	return map[string]any{
		"AgentName":              agentName,
		"ModelName":              modelDisplayName(modelID),
		"EditFileToolsList":      tools.list,
		"EditFileToolsAfterEach": tools.afterEach,
	}
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

type fileEditTools struct {
	list      string
	afterEach string
}

func decideFileEditTools(modelID llmmodel.ModelID) fileEditTools {
	if modelID.ProviderID() == llmmodel.ProviderIDOpenAI {
		return fileEditTools{
			list:      "`apply_patch`",
			afterEach: "`apply_patch`",
		}
	}
	return fileEditTools{
		list:      "`edit`, `write`, and `delete`",
		afterEach: "file-edit tool call (`edit`, `write`, or `delete`)",
	}
}
