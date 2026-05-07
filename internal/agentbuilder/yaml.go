package agentbuilder

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/agentregistry"
	"github.com/codalotl/codalotl/internal/codeunit"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/prompt"
	"github.com/codalotl/codalotl/internal/q/cmdrunner"
	"github.com/codalotl/codalotl/internal/skills"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/codalotl/codalotl/internal/tools/coretools"
	"github.com/codalotl/codalotl/internal/tools/toolsetinterface"
	"gopkg.in/yaml.v3"
)

// YAML constants define built-in YAML names, modes, result formats, and optional tool names.
const (
	yamlAgentModeGeneric        = "generic"                   // Generic mode builds an agent without package-mode authorization.
	yamlAgentModePackage        = "package"                   // Package mode builds an agent with package-mode authorization and package tooling.
	yamlDefaultConfigPath       = "data/config.yml"           // Default config path identifies the embedded YAML configuration.
	yamlPromptBase              = "base"                      // Base prompt selects the generic built-in system prompt.
	yamlPromptPackageBase       = "package-base"              // Package base prompt selects the full Go package-mode system prompt.
	yamlPromptLimitedPkgBase    = "limited-package-base"      // Limited package base prompt selects the limited Go package-mode system prompt.
	yamlToolVirtualEditFiles    = "edit_files"                // Edit files is a virtual tool name expanded to model-specific edit tools.
	yamlRelationDirectImport    = "direct_import_of_caller"   // Direct import relation requires the target package to be imported by the caller package.
	yamlRelationDirectImporter  = "direct_importer_of_caller" // Direct importer relation requires the target package to import the caller package.
	yamlResultFormatText        = "text"                      // Text result format returns the subagent answer unchanged.
	yamlResultFormatJSON        = "json"                      // JSON result format parses and re-emits the subagent answer as normalized JSON.
	yamlOptionalToolCodalotlCLI = "codalotl_cli"              // Codalotl CLI is an allowlisted optional external tool.
	yamlOptionalToolRefactor    = "refactor"                  // Refactor is an allowlisted optional external tool.
)

//go:embed data/*
var embeddedYAMLData embed.FS

// A yamlRegistrySpec is the top-level YAML registry document decoded from an agents and tools file.
type yamlRegistrySpec struct {
	Agents []yamlAgentSpec `yaml:"agents"` // Agents are the agent definitions to validate and register in file order.
	Tools  []yamlToolSpec  `yaml:"tools"`  // Tools are the tool definitions to validate and register before agents.
}

// A yamlAgentSpec describes one agent loaded from YAML.
type yamlAgentSpec struct {
	Name    string          `yaml:"name"`    // Name is the stable agent identifier.
	Prompts []yamlPromptRef `yaml:"prompts"` // Prompts are the ordered prompt blocks used to build the system prompt.
	Tools   []string        `yaml:"tools"`   // Tools are the ordered tool names available to the agent.
	Mode    string          `yaml:"mode"`    // Mode selects whether the agent runs in generic or package mode.

	// IncludePackageModeContext adds environment and initial package context for package-mode agents.
	IncludePackageModeContext bool `yaml:"include_package_mode_context"`

	// Skills enables skill instructions when true or omitted.
	Skills *bool `yaml:"skills"`

	// AgentsMD enables AGENTS.md initial-turn context when true or omitted.
	AgentsMD *bool `yaml:"agentsmd"`
}

// yamlPromptRef selects one text source for a YAML agent prompt.
type yamlPromptRef struct {
	Name string `yaml:"name"` // Name refers to a named prompt.
	File string `yaml:"file"` // File is a prompt file path resolved relative to the YAML file.
	Text string `yaml:"text"` // Text is literal prompt text.
}

// A yamlToolSpec describes one tool entry loaded from a YAML registry spec.
type yamlToolSpec struct {
	Name        string                       `yaml:"name"`        // Name is the required registered tool name.
	Description string                       `yaml:"description"` // Description is the user-facing tool description exposed to the LLM.
	Parameters  map[string]yamlToolParameter `yaml:"parameters"`  // Parameters defines named tool parameters keyed by parameter name.
	Presenter   *yamlPresenterSpec           `yaml:"presenter"`   // Presenter is the optional semantic presentation configuration.
	Command     *yamlCommandSpec             `yaml:"command"`     // Command configures a command tool and is mutually exclusive with Subagent.
	Subagent    *yamlSubagentSpec            `yaml:"subagent"`    // Subagent configures a subagent tool and is mutually exclusive with Command.
}

// yamlToolParameter describes one named parameter for a YAML-defined tool.
type yamlToolParameter struct {
	Type        string `yaml:"type"`        // Type is the JSON-schema parameter type to normalize.
	Description string `yaml:"description"` // Description is the user-facing parameter description.
	Required    bool   `yaml:"required"`    // Required reports whether tool calls must include a non-null value.
}

// yamlCommandSpec describes a templated command to run for a YAML-defined tool or message.
type yamlCommandSpec struct {
	Cmd  string   `yaml:"cmd"`  // Cmd is the required executable or command name template.
	Args []string `yaml:"args"` // Args are optional argument templates rendered in order.
	CWD  string   `yaml:"cwd"`  // CWD is an optional working-directory template.
}

// A yamlSubagentSpec describes a YAML tool that invokes another agent.
type yamlSubagentSpec struct {
	Name         string                    `yaml:"name"`          // Name is the target agent name.
	Package      string                    `yaml:"package"`       // Package names the string parameter that supplies the target package.
	Message      string                    `yaml:"message"`       // Message is a single user message template.
	Messages     []yamlSubagentMessageSpec `yaml:"messages"`      // Messages are the ordered user message sources sent to the subagent.
	ResultFormat string                    `yaml:"result_format"` // ResultFormat selects how the final subagent answer is normalized.

	// PackageRestrictions constrains package targets for package-mode subagents.
	PackageRestrictions *yamlSubagentPackageRestrictions `yaml:"package_restrictions"`
}

// yamlSubagentMessageSpec selects one source for a user message sent to a YAML subagent.
type yamlSubagentMessageSpec struct {
	Name    string           `yaml:"name"`    // Name refers to a named message template.
	File    string           `yaml:"file"`    // File is a message template file resolved relative to the YAML file.
	Text    string           `yaml:"text"`    // Text is a literal message template.
	Command *yamlCommandSpec `yaml:"command"` // Command runs a templated command whose output becomes the message body.
}

// yamlSubagentPackageRestrictions configures package-target constraints for YAML subagent tools.
type yamlSubagentPackageRestrictions struct {
	DisallowSelf        bool   `yaml:"disallow_self"`         // DisallowSelf rejects the caller package as the target package.
	Relation            string `yaml:"relation"`              // Relation requires a direct import relationship; empty means no relationship restriction.
	AllowOutsideSandbox bool   `yaml:"allow_outside_sandbox"` // AllowOutsideSandbox permits resolved target packages outside the sandbox.
	RequirePackageMode  bool   `yaml:"require_package_mode"`  // RequirePackageMode requires the caller to be a package-mode agent.
}

// A yamlNormalizedToolSpec describes a validated YAML-defined tool ready for registration.
type yamlNormalizedToolSpec struct {
	Name              string                             // Name is the registered tool name.
	Description       string                             // Description is the user-facing tool description exposed to the LLM.
	Parameters        map[string]yamlNormalizedParameter // Parameters are normalized parameter definitions keyed by parameter name.
	Presenter         *yamlNormalizedPresenterSpec       // Presenter is the optional normalized semantic presentation configuration.
	Command           *yamlCommandSpec                   // Command is the command-tool configuration; it is nil for subagent tools.
	Subagent          *yamlNormalizedSubagentSpec        // Subagent is the subagent-tool configuration; it is nil for command tools.
	TargetPackageMode bool                               // TargetPackageMode reports whether Subagent targets a package-mode agent.
}

// yamlNormalizedParameter describes a validated YAML tool parameter.
type yamlNormalizedParameter struct {
	Type        string // Type is the normalized JSON-schema parameter type.
	Description string // Description is the user-facing parameter description.
	Required    bool   // Required reports whether tool calls must include a non-null value.
}

// A yamlNormalizedSubagentSpec describes a validated YAML subagent tool target.
type yamlNormalizedSubagentSpec struct {
	Name                string                              // Name is the target agent name.
	Package             string                              // Package names the string parameter that supplies the target package.
	Messages            []yamlNormalizedSubagentMessageSpec // Messages are the ordered user messages sent to the subagent.
	ResultFormat        string                              // ResultFormat is the normalized format applied to the final subagent answer.
	PackageRestrictions *yamlSubagentPackageRestrictions    // PackageRestrictions constrains package targets for package-mode subagents.
}

// yamlNormalizedSubagentMessageSpec is a validated subagent message source.
type yamlNormalizedSubagentMessageSpec struct {
	Text    string           // Text is the resolved message template used when Command is nil.
	Command *yamlCommandSpec // Command is the validated command used to produce the message body when non-nil.
}

// A yamlPreparedAgent contains a validated YAML agent ready for registry registration.
type yamlPreparedAgent struct {
	Definition agentregistry.Definition // Definition is the agent registry definition built from the YAML agent spec.
}

// yamlCommandTool implements a command-backed YAML-defined tool.
type yamlCommandTool struct {
	info      llmstream.ToolInfo                 // Info is the tool metadata exposed to the model.
	presenter llmstream.Presenter                // Presenter formats tool calls and results when the YAML spec configures one.
	spec      *yamlCommandSpec                   // Spec is the command template to render and run.
	params    map[string]yamlNormalizedParameter // Params are the normalized parameter definitions used to parse tool calls.
	opts      toolsetinterface.Options           // Opts are the toolset options used while rendering templates and running the command.
}

// A yamlSubagentTool implements a YAML-defined tool by invoking another agent.
type yamlSubagentTool struct {
	info              llmstream.ToolInfo                 // Info is the metadata advertised to the LLM.
	presenter         llmstream.Presenter                // Presenter renders semantic call and result output, or nil for default presentation.
	spec              *yamlNormalizedSubagentSpec        // Spec is the normalized target-agent configuration.
	params            map[string]yamlNormalizedParameter // Params are the normalized parameter definitions used to parse and render tool calls.
	opts              toolsetinterface.Options           // Opts are the toolset options inherited from the calling agent.
	targetPackageMode bool                               // TargetPackageMode reports whether the target agent requires package-mode invocation.
}

var _ llmstream.Tool = (*yamlCommandTool)(nil)
var _ llmstream.Tool = (*yamlSubagentTool)(nil)

// resolvedPackageTarget identifies a package selected for a YAML subagent invocation.
type resolvedPackageTarget struct {
	AbsDir        string // AbsDir is the absolute directory of the resolved package.
	ImportPath    string // ImportPath is the fully qualified import path, when known.
	ModuleAbsDir  string // ModuleAbsDir is the absolute module root, when known.
	PackageRelDir string // PackageRelDir is the module-relative package directory, when known.
	WithinSandbox bool   // WithinSandbox reports whether AbsDir is inside the caller sandbox.
}

// AddYAMLToRegistry adds agents and tools to reg based on the YAML file at path. If an error occurs, reg will not be mutated.
//
// Errors are returned for typical issues reading the YAML file, and also:
//   - If an agent/tool's name overwrites an existing agent/tool name.
func AddYAMLToRegistry(reg *agentregistry.Registry, path string) error {
	if reg == nil {
		return errors.New("registry is required")
	}
	if strings.TrimSpace(path) == "" {
		return errors.New("yaml path is required")
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("make yaml path absolute: %w", err)
	}

	return addYAMLToRegistryFS(reg, os.DirFS(filepath.Dir(absPath)), filepath.Base(absPath), absPath)
}

func addEmbeddedYAMLToRegistry(reg *agentregistry.Registry) error {
	return addYAMLToRegistryFS(reg, embeddedYAMLData, yamlDefaultConfigPath, yamlDefaultConfigPath)
}

// The addYAMLToRegistryFS function loads one YAML registry document from yamlFS and registers its tools and agents in reg.
//
// The yamlPath argument is resolved within yamlFS, and displayPath is used only in diagnostics. reg and yamlFS must be non-nil. Tools are registered before agents,
// preserving YAML order; loading and validation errors occur before registration and leave reg unchanged.
func addYAMLToRegistryFS(reg *agentregistry.Registry, yamlFS fs.FS, yamlPath string, displayPath string) error {
	spec, yamlDir, err := loadYAMLRegistrySpecFS(yamlFS, yamlPath, displayPath)
	if err != nil {
		return err
	}

	existingToolNames := registryToolNames(reg)
	existingAgentModes := registryAgentModes(reg)

	newAgentModes := make(map[string]string, len(spec.Agents))
	for _, agentSpec := range spec.Agents {
		mode, err := validateYAMLAgentHeader(agentSpec)
		if err != nil {
			return fmt.Errorf("agent %q: %w", agentSpec.Name, err)
		}
		if _, exists := existingAgentModes[agentSpec.Name]; exists {
			return fmt.Errorf("agent %q already exists", agentSpec.Name)
		}
		if _, exists := newAgentModes[agentSpec.Name]; exists {
			return fmt.Errorf("agent %q is defined more than once", agentSpec.Name)
		}
		newAgentModes[agentSpec.Name] = mode
	}

	newToolNames := make(map[string]struct{}, len(spec.Tools))
	normalizedTools := make([]yamlNormalizedToolSpec, 0, len(spec.Tools))
	for _, toolSpec := range spec.Tools {
		if strings.TrimSpace(toolSpec.Name) == "" {
			return errors.New("tool name is required")
		}
		if _, exists := existingToolNames[toolSpec.Name]; exists {
			return fmt.Errorf("tool %q already exists", toolSpec.Name)
		}
		if _, exists := newToolNames[toolSpec.Name]; exists {
			return fmt.Errorf("tool %q is defined more than once", toolSpec.Name)
		}
		normalized, err := normalizeYAMLToolSpec(toolSpec, yamlFS, yamlDir, existingAgentModes, newAgentModes)
		if err != nil {
			return fmt.Errorf("tool %q: %w", toolSpec.Name, err)
		}
		newToolNames[toolSpec.Name] = struct{}{}
		normalizedTools = append(normalizedTools, normalized)
	}

	preparedAgents := make([]yamlPreparedAgent, 0, len(spec.Agents))
	for _, agentSpec := range spec.Agents {
		prepared, err := prepareYAMLAgent(agentSpec, yamlFS, yamlDir, existingToolNames, newToolNames)
		if err != nil {
			return fmt.Errorf("agent %q: %w", agentSpec.Name, err)
		}
		preparedAgents = append(preparedAgents, prepared)
	}

	for _, toolSpec := range normalizedTools {
		if err := reg.RegisterTool(toolSpec.Name, buildYAMLToolBuilder(toolSpec)); err != nil {
			return err
		}
	}
	for _, prepared := range preparedAgents {
		if err := reg.RegisterAgent(prepared.Definition); err != nil {
			return err
		}
	}

	return nil
}

func loadYAMLRegistrySpec(path string) (yamlRegistrySpec, string, error) {
	if strings.TrimSpace(path) == "" {
		return yamlRegistrySpec{}, "", errors.New("yaml path is required")
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return yamlRegistrySpec{}, "", fmt.Errorf("make yaml path absolute: %w", err)
	}

	return loadYAMLRegistrySpecFS(os.DirFS(filepath.Dir(absPath)), filepath.Base(absPath), absPath)
}

// The loadYAMLRegistrySpecFS function reads one YAML registry document from yamlFS and returns the decoded spec and the directory containing path.
//
// The path argument is resolved within yamlFS, and displayPath is used only in diagnostics. The decoder rejects unknown fields and multiple YAML documents.
func loadYAMLRegistrySpecFS(yamlFS fs.FS, path string, displayPath string) (yamlRegistrySpec, string, error) {
	content, err := fs.ReadFile(yamlFS, path)
	if err != nil {
		return yamlRegistrySpec{}, "", fmt.Errorf("read yaml file %q: %w", displayPath, err)
	}

	var spec yamlRegistrySpec
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)
	if err := decoder.Decode(&spec); err != nil {
		return yamlRegistrySpec{}, "", fmt.Errorf("decode yaml file %q: %w", displayPath, err)
	}

	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return yamlRegistrySpec{}, "", fmt.Errorf("decode yaml file %q: multiple yaml documents are not supported", displayPath)
	} else if !errors.Is(err, io.EOF) {
		return yamlRegistrySpec{}, "", fmt.Errorf("decode yaml file %q: %w", displayPath, err)
	}

	return spec, filepath.Dir(path), nil
}

func validateYAMLAgentHeader(spec yamlAgentSpec) (string, error) {
	if strings.TrimSpace(spec.Name) == "" {
		return "", errors.New("name is required")
	}

	switch spec.Mode {
	case yamlAgentModeGeneric, yamlAgentModePackage:
	default:
		return "", fmt.Errorf("invalid mode %q", spec.Mode)
	}

	return spec.Mode, nil
}

// normalizeYAMLToolSpec validates a YAML tool definition and returns a normalized tool specification ready for registration. It normalizes parameters and presentation,
// requires exactly one of command or subagent, validates subagent package targeting against known agent modes, and resolves subagent messages relative to yamlFS
// and yamlDir.
func normalizeYAMLToolSpec(spec yamlToolSpec, yamlFS fs.FS, yamlDir string, existingAgentModes map[string]string, newAgentModes map[string]string) (yamlNormalizedToolSpec, error) {
	if strings.TrimSpace(spec.Description) == "" {
		return yamlNormalizedToolSpec{}, errors.New("description is required")
	}
	if spec.Parameters == nil {
		return yamlNormalizedToolSpec{}, errors.New("parameters is required")
	}

	normalizedParams := make(map[string]yamlNormalizedParameter, len(spec.Parameters))
	for name, param := range spec.Parameters {
		if strings.TrimSpace(name) == "" {
			return yamlNormalizedToolSpec{}, errors.New("parameter name is required")
		}
		paramType, err := normalizeYAMLParameterType(param.Type)
		if err != nil {
			return yamlNormalizedToolSpec{}, fmt.Errorf("parameter %q: %w", name, err)
		}
		if strings.TrimSpace(param.Description) == "" {
			return yamlNormalizedToolSpec{}, fmt.Errorf("parameter %q: description is required", name)
		}
		normalizedParams[name] = yamlNormalizedParameter{
			Type:        paramType,
			Description: param.Description,
			Required:    param.Required,
		}
	}

	normalizedPresenter, err := normalizeYAMLPresenterSpec(spec.Presenter, normalizedParams)
	if err != nil {
		return yamlNormalizedToolSpec{}, err
	}

	if (spec.Command == nil) == (spec.Subagent == nil) {
		return yamlNormalizedToolSpec{}, errors.New("exactly one of command or subagent is required")
	}

	if spec.Command != nil {
		if strings.TrimSpace(spec.Command.Cmd) == "" {
			return yamlNormalizedToolSpec{}, errors.New("command.cmd is required")
		}
		return yamlNormalizedToolSpec{
			Name:        spec.Name,
			Description: spec.Description,
			Parameters:  normalizedParams,
			Presenter:   normalizedPresenter,
			Command:     spec.Command,
		}, nil
	}

	if strings.TrimSpace(spec.Subagent.Name) == "" {
		return yamlNormalizedToolSpec{}, errors.New("subagent.name is required")
	}

	targetMode, ok := newAgentModes[spec.Subagent.Name]
	if !ok {
		targetMode, ok = existingAgentModes[spec.Subagent.Name]
	}
	if !ok {
		return yamlNormalizedToolSpec{}, fmt.Errorf("subagent %q is not registered", spec.Subagent.Name)
	}

	if spec.Subagent.Package != "" {
		param, ok := normalizedParams[spec.Subagent.Package]
		if !ok {
			return yamlNormalizedToolSpec{}, fmt.Errorf("subagent.package refers to unknown parameter %q", spec.Subagent.Package)
		}
		if param.Type != "string" {
			return yamlNormalizedToolSpec{}, fmt.Errorf("subagent.package parameter %q must be type string", spec.Subagent.Package)
		}
	}

	if targetMode == yamlAgentModePackage && spec.Subagent.Package == "" {
		return yamlNormalizedToolSpec{}, fmt.Errorf("package-mode subagent %q requires subagent.package", spec.Subagent.Name)
	}
	if targetMode != yamlAgentModePackage && spec.Subagent.Package != "" {
		return yamlNormalizedToolSpec{}, fmt.Errorf("non-package subagent %q must not set subagent.package", spec.Subagent.Name)
	}

	if spec.Subagent.PackageRestrictions != nil {
		switch spec.Subagent.PackageRestrictions.Relation {
		case "", yamlRelationDirectImport, yamlRelationDirectImporter:
		default:
			return yamlNormalizedToolSpec{}, fmt.Errorf("invalid package_restrictions.relation %q", spec.Subagent.PackageRestrictions.Relation)
		}
	}

	normalizedSubagent, err := normalizeYAMLSubagentSpec(spec.Subagent, yamlFS, yamlDir)
	if err != nil {
		return yamlNormalizedToolSpec{}, err
	}

	return yamlNormalizedToolSpec{
		Name:              spec.Name,
		Description:       spec.Description,
		Parameters:        normalizedParams,
		Presenter:         normalizedPresenter,
		Subagent:          &normalizedSubagent,
		TargetPackageMode: targetMode == yamlAgentModePackage,
	}, nil
}

// normalizeYAMLSubagentSpec validates and normalizes a YAML subagent spec. It requires spec to be non-nil, resolves subagent messages against yamlFS and yamlDir,
// defaults an empty result_format to text, and returns an error for invalid messages or unsupported result formats.
func normalizeYAMLSubagentSpec(spec *yamlSubagentSpec, yamlFS fs.FS, yamlDir string) (yamlNormalizedSubagentSpec, error) {
	if spec == nil {
		return yamlNormalizedSubagentSpec{}, errors.New("subagent is required")
	}

	normalizedMessages, err := normalizeYAMLSubagentMessages(spec, yamlFS, yamlDir)
	if err != nil {
		return yamlNormalizedSubagentSpec{}, err
	}

	resultFormat, err := normalizeYAMLSubagentResultFormat(spec.ResultFormat)
	if err != nil {
		return yamlNormalizedSubagentSpec{}, err
	}

	return yamlNormalizedSubagentSpec{
		Name:                spec.Name,
		Package:             spec.Package,
		Messages:            normalizedMessages,
		ResultFormat:        resultFormat,
		PackageRestrictions: spec.PackageRestrictions,
	}, nil
}

// normalizeYAMLSubagentMessages validates and normalizes the user messages for a YAML subagent. A subagent must set exactly one of message or messages; message
// becomes a single inline template, while messages entries are resolved from their selected source.
func normalizeYAMLSubagentMessages(spec *yamlSubagentSpec, yamlFS fs.FS, yamlDir string) ([]yamlNormalizedSubagentMessageSpec, error) {
	hasMessage := strings.TrimSpace(spec.Message) != ""
	hasMessages := len(spec.Messages) > 0
	if hasMessage == hasMessages {
		return nil, errors.New("exactly one of subagent.message or subagent.messages is required")
	}

	if hasMessage {
		return []yamlNormalizedSubagentMessageSpec{{Text: spec.Message}}, nil
	}

	normalized := make([]yamlNormalizedSubagentMessageSpec, 0, len(spec.Messages))
	for i, message := range spec.Messages {
		resolved, err := normalizeYAMLSubagentMessageSpec(message, yamlFS, yamlDir)
		if err != nil {
			return nil, fmt.Errorf("subagent.messages[%d]: %w", i, err)
		}
		normalized = append(normalized, resolved)
	}
	return normalized, nil
}

// normalizeYAMLSubagentMessageSpec validates one YAML subagent message source and resolves it to a normalized message. It returns an error unless exactly one of
// name, file, text, or command is set; command sources must include command.cmd. Name, file, and text sources are resolved to Text, while command sources are preserved
// for execution at call time.
func normalizeYAMLSubagentMessageSpec(spec yamlSubagentMessageSpec, yamlFS fs.FS, yamlDir string) (yamlNormalizedSubagentMessageSpec, error) {
	count := 0
	if strings.TrimSpace(spec.Name) != "" {
		count++
	}
	if strings.TrimSpace(spec.File) != "" {
		count++
	}
	if strings.TrimSpace(spec.Text) != "" {
		count++
	}
	if spec.Command != nil {
		count++
	}
	if count != 1 {
		return yamlNormalizedSubagentMessageSpec{}, errors.New("must set exactly one of name, file, text, or command")
	}

	if spec.Command != nil {
		if strings.TrimSpace(spec.Command.Cmd) == "" {
			return yamlNormalizedSubagentMessageSpec{}, errors.New("command.cmd is required")
		}
		return yamlNormalizedSubagentMessageSpec{Command: spec.Command}, nil
	}

	text, err := resolveYAMLTextSource(yamlFS, yamlDir, spec.Name, spec.File, spec.Text)
	if err != nil {
		return yamlNormalizedSubagentMessageSpec{}, err
	}
	return yamlNormalizedSubagentMessageSpec{Text: text}, nil
}

func normalizeYAMLSubagentResultFormat(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return yamlResultFormatText, nil
	}
	switch raw {
	case yamlResultFormatText, yamlResultFormatJSON:
		return raw, nil
	default:
		return "", fmt.Errorf("unsupported subagent.result_format %q", raw)
	}
}

// prepareYAMLAgent validates and resolves a YAML agent spec into a registry-ready prepared agent. It resolves prompt and tool references against yamlFS and yamlDir,
// applies YAML defaults for skills and AGENTS.md, configures package-mode behavior, and returns errors for invalid or unresolved configuration.
func prepareYAMLAgent(spec yamlAgentSpec, yamlFS fs.FS, yamlDir string, existingToolNames map[string]struct{}, newToolNames map[string]struct{}) (yamlPreparedAgent, error) {
	if len(spec.Prompts) == 0 {
		return yamlPreparedAgent{}, errors.New("prompts is required")
	}
	if len(spec.Tools) == 0 {
		return yamlPreparedAgent{}, errors.New("tools is required")
	}
	if spec.IncludePackageModeContext && spec.Mode != yamlAgentModePackage {
		return yamlPreparedAgent{}, errors.New("include_package_mode_context is only valid for package mode agents")
	}

	resolvedPrompt, err := resolveYAMLAgentPrompt(yamlFS, yamlDir, spec.Prompts)
	if err != nil {
		return yamlPreparedAgent{}, err
	}

	resolvedToolNames, err := validateYAMLAgentTools(spec.Tools, existingToolNames, newToolNames)
	if err != nil {
		return yamlPreparedAgent{}, err
	}

	enableSkills := spec.Skills == nil || *spec.Skills
	if enableSkills && !yamlToolNamesContainSkillShell(resolvedToolNames) {
		return yamlPreparedAgent{}, errors.New("skills require shell or skill_shell to be present")
	}
	enableAgentsMD := spec.AgentsMD == nil || *spec.AgentsMD

	prepared := yamlPreparedAgent{}
	prepared.Definition = agentregistry.Definition{
		Name:        spec.Name,
		Description: "YAML-defined " + spec.Mode + " agent.",
		ToolsBuilder: func(opts toolsetinterface.Options) ([]string, error) {
			return expandYAMLToolNames(resolvedToolNames, opts.Model), nil
		},
		SystemPromptBuilder: func(options agentregistry.BuildOptions) (string, error) {
			return buildYAMLAgentSystemPrompt(options, resolvedPrompt, enableSkills, spec.Mode == yamlAgentModePackage, resolvedToolNames)
		},
	}
	if spec.Mode == yamlAgentModePackage {
		prepared.Definition.AuthPolicy = agentregistry.AuthPolicyPackage
	}
	if initialTurnsBuilder := buildYAMLAgentInitialTurnsBuilder(spec.Mode, spec.IncludePackageModeContext, enableAgentsMD); initialTurnsBuilder != nil {
		prepared.Definition.InitialTurnsBuilder = initialTurnsBuilder
	}
	if err := prepared.Definition.Validate(); err != nil {
		return yamlPreparedAgent{}, err
	}

	return prepared, nil
}

func buildYAMLAgentInitialTurnsBuilder(mode string, includePackageModeContext bool, enableAgentsMD bool) agentregistry.InitialTurnsBuilder {
	switch {
	case mode == yamlAgentModePackage && includePackageModeContext:
		return func(ctx context.Context, options agentregistry.BuildOptions) ([]string, error) {
			return buildPackageModeContextInitialTurns(ctx, options, enableAgentsMD)
		}
	case mode == yamlAgentModePackage && enableAgentsMD:
		return buildPackageModeAgentsMDInitialTurns
	case mode == yamlAgentModeGeneric && enableAgentsMD:
		return buildGenericAgentsMDInitialTurns
	default:
		return nil
	}
}

func resolveYAMLAgentPrompt(yamlFS fs.FS, yamlDir string, prompts []yamlPromptRef) (string, error) {
	blocks := make([]string, 0, len(prompts))
	for i, promptRef := range prompts {
		text, err := resolveYAMLTextSource(yamlFS, yamlDir, promptRef.Name, promptRef.File, promptRef.Text)
		if err != nil {
			return "", fmt.Errorf("prompt %d: %w", i, err)
		}
		blocks = append(blocks, text)
	}

	return joinContextBlocks(blocks...), nil
}

// resolveYAMLTextSource resolves exactly one YAML text source to text. It treats name as a built-in prompt name, file as a path relative to yamlDir in yamlFS, and
// text as inline content. It returns an error unless exactly one source is set and that source can be resolved.
func resolveYAMLTextSource(yamlFS fs.FS, yamlDir string, name string, file string, text string) (string, error) {
	count := 0
	if strings.TrimSpace(name) != "" {
		count++
	}
	if strings.TrimSpace(file) != "" {
		count++
	}
	if strings.TrimSpace(text) != "" {
		count++
	}
	if count != 1 {
		return "", errors.New("must set exactly one of name, file, or text")
	}

	switch {
	case strings.TrimSpace(name) != "":
		return resolveBuiltinYAMLPrompt(name)
	case strings.TrimSpace(file) != "":
		promptPath := filepath.Join(yamlDir, file)
		content, err := fs.ReadFile(yamlFS, promptPath)
		if err != nil {
			return "", fmt.Errorf("read prompt file %q: %w", file, err)
		}
		return string(content), nil
	default:
		return text, nil
	}
}

func resolveBuiltinYAMLPrompt(name string) (string, error) {
	switch name {
	case yamlPromptBase:
		return prompt.GetBasicPrompt(), nil
	case yamlPromptPackageBase:
		return prompt.GetGoPackageModeModePrompt(prompt.GoPackageModePromptKindFull), nil
	case yamlPromptLimitedPkgBase:
		return prompt.GetGoPackageModeModePrompt(prompt.GoPackageModePromptKindUpdateUsage), nil
	default:
		return "", fmt.Errorf("unknown prompt name %q", name)
	}
}

// validateYAMLAgentTools checks a YAML agent tool list and returns the ordered tool names to attach to the agent.
//
// The returned names keep the virtual edit-files tool for later expansion and omit missing optional external tools. Unknown non-optional tools are errors.
func validateYAMLAgentTools(toolNames []string, existingToolNames map[string]struct{}, newToolNames map[string]struct{}) ([]string, error) {
	resolved := make([]string, 0, len(toolNames))
	for _, name := range toolNames {
		switch name {
		case yamlToolVirtualEditFiles:
			resolved = append(resolved, name)
		default:
			if _, ok := newToolNames[name]; ok {
				resolved = append(resolved, name)
				continue
			}
			if _, ok := existingToolNames[name]; ok {
				resolved = append(resolved, name)
				continue
			}
			if yamlOptionalExternalTool(name) {
				continue
			}
			return nil, fmt.Errorf("tool %q is not registered", name)
		}
	}
	return resolved, nil
}

func yamlOptionalExternalTool(name string) bool {
	switch name {
	case yamlOptionalToolCodalotlCLI, yamlOptionalToolRefactor:
		return true
	default:
		return false
	}
}

func buildYAMLAgentSystemPrompt(options agentregistry.BuildOptions, basePrompt string, enableSkills bool, isPackageMode bool, toolNames []string) (string, error) {
	if !enableSkills {
		return basePrompt, nil
	}

	shellToolName := yamlSkillShellToolName(toolNames)
	if shellToolName == "" {
		shellToolName = yamlSkillShellToolName(expandYAMLToolNames(toolNames, options.ToolOptions.Model))
	}
	if shellToolName == "" {
		return "", errors.New("skills require shell or skill_shell")
	}

	return buildSkillsEnabledSystemPrompt(options, basePrompt, shellToolName, isPackageMode)
}

// The buildSkillsEnabledSystemPrompt function appends available skill instructions to basePrompt.
//
// It ensures built-in skills are installed, loads skills from GoPkgAbsDir or SandboxDir, authorizes loaded skill directories when an authorizer is configured, and
// uses shellToolName in the generated skill instructions. Skill discovery failures are treated as no available skills.
func buildSkillsEnabledSystemPrompt(options agentregistry.BuildOptions, basePrompt string, shellToolName string, isPackageMode bool) (string, error) {
	if err := skills.InstallDefault(); err != nil {
		return "", fmt.Errorf("install default skills: %w", err)
	}

	searchDir := options.ToolOptions.GoPkgAbsDir
	if strings.TrimSpace(searchDir) == "" {
		searchDir = options.ToolOptions.SandboxDir
	}

	validSkills, invalidSkills, failedSkillLoads, skillsErr := skills.LoadSkills(skills.SearchPaths(searchDir))
	if skillsErr != nil {
		validSkills = nil
		invalidSkills = nil
		failedSkillLoads = nil
	}
	_ = invalidSkills
	_ = failedSkillLoads

	if options.ToolOptions.Authorizer != nil {
		if err := skills.Authorize(validSkills, options.ToolOptions.Authorizer); err != nil {
			return "", fmt.Errorf("authorize skills: %w", err)
		}
	}

	return joinContextBlocks(
		basePrompt,
		skills.Prompt(validSkills, shellToolName, isPackageMode),
	), nil
}

// buildYAMLToolBuilder returns a tool constructor for a normalized YAML tool specification. The returned constructor builds metadata, required parameter names,
// and any configured presenter, then creates either a command-backed or subagent-backed tool.
func buildYAMLToolBuilder(spec yamlNormalizedToolSpec) toolsetinterface.Tool {
	return func(opts toolsetinterface.Options) (llmstream.Tool, error) {
		info := llmstream.ToolInfo{
			Name:        spec.Name,
			Description: spec.Description,
			Parameters:  make(map[string]any, len(spec.Parameters)),
			Required:    requiredYAMLParameterNames(spec.Parameters),
		}
		for name, param := range spec.Parameters {
			info.Parameters[name] = map[string]any{
				"type":        param.Type,
				"description": param.Description,
			}
		}
		presenter := buildYAMLPresenter(spec.Presenter, spec.Parameters)

		if spec.Command != nil {
			return &yamlCommandTool{
				info:      info,
				presenter: presenter,
				spec:      spec.Command,
				params:    spec.Parameters,
				opts:      opts,
			}, nil
		}

		return &yamlSubagentTool{
			info:              info,
			presenter:         presenter,
			spec:              spec.Subagent,
			params:            spec.Parameters,
			opts:              opts,
			targetPackageMode: spec.TargetPackageMode,
		}, nil
	}
}

func requiredYAMLParameterNames(params map[string]yamlNormalizedParameter) []string {
	required := make([]string, 0, len(params))
	for name, param := range params {
		if param.Required {
			required = append(required, name)
		}
	}
	sort.Strings(required)
	return required
}

func expandYAMLToolNames(toolNames []string, model llmmodel.ModelID) []string {
	expanded := make([]string, 0, len(toolNames)+3)
	for _, name := range toolNames {
		if name == yamlToolVirtualEditFiles {
			expanded = append(expanded, buildEditFileToolNames(model)...)
			continue
		}
		expanded = append(expanded, name)
	}
	return expanded
}

func yamlToolNamesContainSkillShell(toolNames []string) bool {
	return yamlSkillShellToolName(toolNames) != ""
}

func yamlSkillShellToolName(toolNames []string) string {
	for _, name := range toolNames {
		if name == coretools.ToolNameSkillShell {
			return name
		}
	}
	for _, name := range toolNames {
		if name == coretools.ToolNameShell {
			return name
		}
	}
	return ""
}

func normalizeYAMLParameterType(raw string) (string, error) {
	switch raw {
	case "string":
		return "string", nil
	case "bool", "boolean":
		return "boolean", nil
	case "int", "integer":
		return "integer", nil
	default:
		return "", fmt.Errorf("unsupported parameter type %q", raw)
	}
}

func registryToolNames(reg *agentregistry.Registry) map[string]struct{} {
	names := make(map[string]struct{})
	if reg == nil {
		return names
	}

	for _, name := range reg.ListToolNames() {
		names[name] = struct{}{}
	}

	return names
}

func registryAgentModes(reg *agentregistry.Registry) map[string]string {
	modes := make(map[string]string, len(reg.List()))
	for _, def := range reg.List() {
		if def.AuthPolicy == agentregistry.AuthPolicyPackage {
			modes[def.Name] = yamlAgentModePackage
			continue
		}
		modes[def.Name] = yamlAgentModeGeneric
	}
	return modes
}

// runYAMLCommand renders, authorizes, and executes a YAML command spec. The supplied context controls command execution. data supplies template values, and opts
// supplies the sandbox root and optional authorizer. The returned result describes the attempted command execution, and errors report rendering, authorization,
// or runner failures.
func runYAMLCommand(ctx context.Context, spec *yamlCommandSpec, data map[string]any, opts toolsetinterface.Options) (cmdrunner.Result, error) {
	renderedCmd, renderedArgs, cwd, err := renderYAMLCommandSpec(spec, data, opts.SandboxDir)
	if err != nil {
		return cmdrunner.Result{}, err
	}

	command := append([]string{renderedCmd}, renderedArgs...)
	if opts.Authorizer != nil {
		if err := opts.Authorizer.IsShellAuthorized(false, "", cwd, command); err != nil {
			return cmdrunner.Result{}, err
		}
	}

	runner := cmdrunner.NewRunner(nil, nil)
	runner.AddCommand(cmdrunner.Command{
		Command: renderedCmd,
		Args:    renderedArgs,
		CWD:     cwd,
		ShowCWD: true,
	})

	rootDir := opts.SandboxDir
	if strings.TrimSpace(rootDir) == "" {
		rootDir = cwd
	}
	return runner.Run(ctx, rootDir, nil)
}

// renderYAMLCommandSpec renders a YAML command spec with data and returns the command, arguments, and working directory to execute. defaultCWD is used when the
// spec has no cwd; if defaultCWD is empty, the current process working directory is used. It returns an error when the spec is nil, a template cannot be rendered,
// or no working directory can be determined.
func renderYAMLCommandSpec(spec *yamlCommandSpec, data map[string]any, defaultCWD string) (string, []string, string, error) {
	if spec == nil {
		return "", nil, "", errors.New("command is required")
	}

	renderedCmd, err := renderYAMLTemplateString(spec.Cmd, data)
	if err != nil {
		return "", nil, "", fmt.Errorf("render command.cmd: %w", err)
	}

	renderedArgs := make([]string, 0, len(spec.Args))
	for i, arg := range spec.Args {
		renderedArg, err := renderYAMLTemplateString(arg, data)
		if err != nil {
			return "", nil, "", fmt.Errorf("render command.args[%d]: %w", i, err)
		}
		renderedArgs = append(renderedArgs, renderedArg)
	}

	cwd := defaultCWD
	if strings.TrimSpace(cwd) == "" {
		cwd, err = os.Getwd()
		if err != nil {
			return "", nil, "", fmt.Errorf("determine working directory: %w", err)
		}
	}
	if spec.CWD != "" {
		cwd, err = renderYAMLTemplateString(spec.CWD, data)
		if err != nil {
			return "", nil, "", fmt.Errorf("render command.cwd: %w", err)
		}
	}

	return renderedCmd, renderedArgs, cwd, nil
}

// Info returns the tool metadata exposed to the model.
func (t *yamlCommandTool) Info() llmstream.ToolInfo {
	return t.info
}

// Name returns the registered tool name.
func (t *yamlCommandTool) Name() string {
	return t.info.Name
}

// Presenter returns the optional semantic presenter for the tool.
func (t *yamlCommandTool) Presenter() llmstream.Presenter {
	return t.presenter
}

// Run executes the configured YAML command for call. It decodes call.Input with the tool's parameter schema, renders command templates with call parameters and
// context data, and returns either an error ToolResult or the command result serialized as XML.
func (t *yamlCommandTool) Run(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
	params, err := parseYAMLToolCallParams(call.Input, t.params)
	if err != nil {
		return yamlToolErrorResult(call, err)
	}

	data, err := buildYAMLTemplateData(t.opts, params)
	if err != nil {
		return yamlToolErrorResult(call, err)
	}

	result, err := runYAMLCommand(ctx, t.spec, data, t.opts)
	if err != nil {
		return yamlToolErrorResult(call, err)
	}

	return llmstream.ToolResult{
		CallID:  call.CallID,
		Name:    call.Name,
		Type:    call.Type,
		Result:  result.ToXML("command"),
		IsError: !result.Success(),
	}
}

// Info returns the metadata advertised for the YAML subagent tool.
func (t *yamlSubagentTool) Info() llmstream.ToolInfo {
	return t.info
}

// Name returns the registered tool name.
func (t *yamlSubagentTool) Name() string {
	return t.info.Name
}

// Presenter returns the semantic presenter configured for the tool, or nil when default presentation should be used.
func (t *yamlSubagentTool) Presenter() llmstream.Presenter {
	return t.presenter
}

// Run executes the YAML subagent tool call and returns the final subagent answer as a tool result.
//
// Options.AgentInvoker must be set, and ctx controls message rendering, agent invocation, and final-answer collection. Run parses call.Input as JSON using the tool's
// parameter definitions, renders the configured messages with parameter and caller-context template data, invokes the target agent, and normalizes the answer according
// to the configured result format. Rendering may execute command-backed message sources. Any error is returned as an error ToolResult that preserves the call identifiers.
func (t *yamlSubagentTool) Run(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
	if t.opts.AgentInvoker == nil {
		return yamlToolErrorResult(call, errors.New("agent invoker is required"))
	}

	params, err := parseYAMLToolCallParams(call.Input, t.params)
	if err != nil {
		return yamlToolErrorResult(call, err)
	}

	templateData, err := buildYAMLTemplateData(t.opts, params)
	if err != nil {
		return yamlToolErrorResult(call, err)
	}

	messages, err := t.renderMessages(ctx, templateData)
	if err != nil {
		return yamlToolErrorResult(call, err)
	}

	subAgentCreator := yamlSubAgentCreatorFromContextSafe(ctx)
	req, err := t.buildInvokeRequest(messages, params, subAgentCreator)
	if err != nil {
		return yamlToolErrorResult(call, err)
	}
	if req.AgentCreator == nil {
		req.AgentCreator = agent.NewAgentCreator()
	}

	events, err := t.opts.AgentInvoker.Invoke(ctx, t.spec.Name, req)
	if err != nil {
		return yamlToolErrorResult(call, err)
	}

	answer, err := agent.CollectFinalAssistantText(ctx, events)
	if err != nil {
		return yamlToolErrorResult(call, err)
	}

	answer, err = normalizeYAMLSubagentResult(answer, t.spec.ResultFormat)
	if err != nil {
		return yamlToolErrorResult(call, err)
	}

	return llmstream.ToolResult{
		CallID: call.CallID,
		Name:   call.Name,
		Type:   call.Type,
		Result: answer,
	}
}

// renderMessages renders the configured subagent message sources into ordered user messages. Text sources are rendered with templateData, and command sources are
// executed with ctx and the tool options; command sources must succeed and return exactly one result.
func (t *yamlSubagentTool) renderMessages(ctx context.Context, templateData map[string]any) ([]string, error) {
	messages := make([]string, 0, len(t.spec.Messages))
	for i, messageSpec := range t.spec.Messages {
		if messageSpec.Command != nil {
			result, err := runYAMLCommand(ctx, messageSpec.Command, templateData, t.opts)
			if err != nil {
				return nil, fmt.Errorf("run subagent.messages[%d].command: %w", i, err)
			}
			if !result.Success() {
				return nil, fmt.Errorf("run subagent.messages[%d].command: %s", i, result.ToXML("command"))
			}
			if len(result.Results) != 1 {
				return nil, fmt.Errorf("run subagent.messages[%d].command: expected 1 result, got %d", i, len(result.Results))
			}
			messages = append(messages, result.Results[0].Output)
			continue
		}

		message, err := renderYAMLTemplateString(messageSpec.Text, templateData)
		if err != nil {
			return nil, fmt.Errorf("render subagent.messages[%d]: %w", i, err)
		}
		messages = append(messages, message)
	}
	return messages, nil
}

func normalizeYAMLSubagentResult(answer string, resultFormat string) (string, error) {
	switch resultFormat {
	case "", yamlResultFormatText:
		return answer, nil
	case yamlResultFormatJSON:
		var normalized any
		if err := json.Unmarshal([]byte(answer), &normalized); err != nil {
			return "", fmt.Errorf("parse subagent result as json: %w", err)
		}
		data, err := json.Marshal(normalized)
		if err != nil {
			return "", fmt.Errorf("normalize subagent json result: %w", err)
		}
		return string(data), nil
	default:
		return "", fmt.Errorf("unsupported subagent result format %q", resultFormat)
	}
}

// The buildInvokeRequest method builds the invocation request for the configured target agent.
//
// It copies messages, passes through agentCreator, and inherits the caller's tool options and authorization context. For package-mode targets, params must contain
// the configured package parameter; the request is retargeted to the resolved package and uses an authorizer constrained to that package. It returns an error if
// package resolution, restriction checks, or authorizer construction fails.
func (t *yamlSubagentTool) buildInvokeRequest(messages []string, params map[string]any, agentCreator agent.AgentCreator) (toolsetinterface.InvokeRequest, error) {
	req := toolsetinterface.InvokeRequest{
		Messages: append([]string(nil), messages...),
		ToolOptions: toolsetinterface.Options{
			Model:        t.opts.Model,
			Authorizer:   t.opts.Authorizer,
			SandboxDir:   t.opts.SandboxDir,
			GoPkgAbsDir:  t.opts.GoPkgAbsDir,
			LintSteps:    t.opts.LintSteps,
			AgentInvoker: t.opts.AgentInvoker,
		},
		CallerAuthorizer: t.opts.Authorizer,
		CallerSandboxDir: t.opts.SandboxDir,
		AgentCreator:     agentCreator,
	}

	if agentCreator != nil {
		req.AgentCreator = agentCreator
	}

	if t.targetPackageMode {
		targetPackage, err := t.resolveTargetPackage(params)
		if err != nil {
			return toolsetinterface.InvokeRequest{}, err
		}

		req.ToolOptions.GoPkgAbsDir = targetPackage.AbsDir
		overrideSandboxDir := t.opts.SandboxDir
		if !targetPackage.WithinSandbox {
			overrideSandboxDir = targetPackage.ModuleAbsDir
			if overrideSandboxDir == "" {
				overrideSandboxDir = targetPackage.AbsDir
			}
		}
		if overrideSandboxDir == "" {
			overrideSandboxDir = targetPackage.AbsDir
		}

		req.CallerSandboxDir = overrideSandboxDir
		req.ToolOptions.SandboxDir = overrideSandboxDir
		targetAuthorizer, err := t.buildTargetPackageAuthorizer(targetPackage, overrideSandboxDir)
		if err != nil {
			return toolsetinterface.InvokeRequest{}, err
		}
		req.CallerAuthorizer = targetAuthorizer
		req.ToolOptions.Authorizer = req.CallerAuthorizer
	}

	return req, nil
}

// resolveTargetPackage resolves the subagent package parameter and applies the tool's package restrictions. It requires a non-empty string value for the configured
// package parameter, rejects packages outside the sandbox unless allowed, and for package-mode callers can enforce self-package and direct-import relationship constraints.
func (t *yamlSubagentTool) resolveTargetPackage(params map[string]any) (resolvedPackageTarget, error) {
	if t.spec.Package == "" {
		return resolvedPackageTarget{}, errors.New("subagent package parameter is required")
	}

	rawValue, _ := params[t.spec.Package].(string)
	if strings.TrimSpace(rawValue) == "" {
		return resolvedPackageTarget{}, fmt.Errorf("subagent package parameter %q is required", t.spec.Package)
	}

	target, err := resolveYAMLTargetPackage(t.opts, rawValue)
	if err != nil {
		return resolvedPackageTarget{}, err
	}

	restrictions := t.spec.PackageRestrictions
	if restrictions == nil {
		restrictions = &yamlSubagentPackageRestrictions{}
	}

	if !target.WithinSandbox && !restrictions.AllowOutsideSandbox {
		return resolvedPackageTarget{}, fmt.Errorf("package %q is outside the sandbox", rawValue)
	}

	callerIsPackageMode := isPackageModeAgent(t.opts.AgentName)
	if restrictions.RequirePackageMode && !callerIsPackageMode {
		return resolvedPackageTarget{}, errors.New("caller must be in package mode")
	}

	if callerIsPackageMode {
		currentPkg, err := loadCurrentPackage(t.opts)
		if err != nil {
			return resolvedPackageTarget{}, err
		}

		if restrictions.DisallowSelf && yamlTargetsSamePackage(currentPkg, target) {
			return resolvedPackageTarget{}, errors.New("target package must differ from caller package")
		}

		switch restrictions.Relation {
		case "":
		case yamlRelationDirectImport:
			if target.ImportPath == "" {
				return resolvedPackageTarget{}, fmt.Errorf("could not resolve import path for target package %q", rawValue)
			}
			if _, ok := currentPkg.ImportPaths[target.ImportPath]; !ok {
				return resolvedPackageTarget{}, fmt.Errorf("target package %q is not a direct import of caller", rawValue)
			}
		case yamlRelationDirectImporter:
			targetPkg, err := loadResolvedPackage(target)
			if err != nil {
				return resolvedPackageTarget{}, err
			}
			if _, ok := targetPkg.ImportPaths[currentPkg.ImportPath]; !ok {
				return resolvedPackageTarget{}, fmt.Errorf("target package %q is not a direct importer of caller", rawValue)
			}
		default:
			return resolvedPackageTarget{}, fmt.Errorf("unsupported package relation %q", restrictions.Relation)
		}
	}

	return target, nil
}

// buildTargetPackageAuthorizer returns an authorizer that restricts access to the resolved target package.
func (t *yamlSubagentTool) buildTargetPackageAuthorizer(target resolvedPackageTarget, sandboxDir string) (authdomain.Authorizer, error) {
	unit, err := codeunit.DefaultGoCodeUnit(target.AbsDir)
	if err != nil {
		return nil, fmt.Errorf("build default go code unit for target package %q: %w", target.AbsDir, err)
	}

	fallback := authdomain.NewAutoApproveAuthorizer(sandboxDir)
	if target.WithinSandbox && t.opts.Authorizer != nil {
		fallback = t.opts.Authorizer.WithoutCodeUnit()
	}

	return authdomain.NewCodeUnitAuthorizer(unit, fallback), nil
}

// parseYAMLToolCallParams decodes and validates a YAML-defined tool call's JSON input.
func parseYAMLToolCallParams(raw string, paramSpecs map[string]yamlNormalizedParameter) (map[string]any, error) {
	if strings.TrimSpace(raw) == "" {
		raw = "{}"
	}

	var rawParams map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &rawParams); err != nil {
		return nil, fmt.Errorf("parse tool input: %w", err)
	}

	params := make(map[string]any, len(paramSpecs))
	for key, value := range rawParams {
		paramSpec, ok := paramSpecs[key]
		if !ok {
			return nil, fmt.Errorf("unknown parameter %q", key)
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			if paramSpec.Required {
				return nil, fmt.Errorf("parameter %q is required", key)
			}
			continue
		}

		parsedValue, err := parseYAMLParameterValue(value, paramSpec.Type)
		if err != nil {
			return nil, fmt.Errorf("parameter %q: %w", key, err)
		}
		params[key] = parsedValue
	}

	for name, spec := range paramSpecs {
		if spec.Required {
			if _, ok := params[name]; !ok {
				return nil, fmt.Errorf("parameter %q is required", name)
			}
		}
	}

	return params, nil
}

// The parseYAMLParameterValue function decodes one raw JSON tool parameter according to a YAML parameter type.
//
// Supported types are "string", "boolean", and "integer"; the returned value has type string, bool, or int. It returns an error for unsupported types or JSON that
// cannot be decoded as the requested type.
func parseYAMLParameterValue(raw json.RawMessage, paramType string) (any, error) {
	switch paramType {
	case "string":
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, err
		}
		return value, nil
	case "boolean":
		var value bool
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, err
		}
		return value, nil
	case "integer":
		var value int
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, err
		}
		return value, nil
	default:
		return nil, fmt.Errorf("unsupported parameter type %q", paramType)
	}
}

func buildYAMLTemplateData(opts toolsetinterface.Options, params map[string]any) (map[string]any, error) {
	data := make(map[string]any, len(params)+2)
	for name, value := range params {
		data[name] = value
	}

	sandboxDir := opts.SandboxDir
	if sandboxDir != "" {
		absSandbox, err := filepath.Abs(sandboxDir)
		if err != nil {
			return nil, fmt.Errorf("make sandbox dir absolute: %w", err)
		}
		sandboxDir = absSandbox
	}
	data["sandbox_dir"] = sandboxDir
	data["package_dir"] = yamlCurrentPackageDir(opts, sandboxDir)

	return data, nil
}

func yamlCurrentPackageDir(opts toolsetinterface.Options, absSandboxDir string) string {
	if absSandboxDir == "" || opts.GoPkgAbsDir == "" {
		return ""
	}

	absPkgDir, err := filepath.Abs(opts.GoPkgAbsDir)
	if err != nil {
		return ""
	}
	if !clarifyPathWithinDir(absSandboxDir, absPkgDir) {
		return ""
	}

	rel, err := filepath.Rel(absSandboxDir, absPkgDir)
	if err != nil {
		return ""
	}
	return filepath.ToSlash(rel)
}

func renderYAMLTemplateString(raw string, data map[string]any) (string, error) {
	tmpl, err := template.New("yaml-tool").Option("missingkey=error").Parse(raw)
	if err != nil {
		return "", err
	}

	var out bytes.Buffer
	if err := tmpl.Execute(&out, data); err != nil {
		return "", err
	}
	return out.String(), nil
}

func yamlToolErrorResult(call llmstream.ToolCall, err error) llmstream.ToolResult {
	return llmstream.ToolResult{
		CallID:    call.CallID,
		Name:      call.Name,
		Type:      call.Type,
		Result:    err.Error(),
		IsError:   true,
		SourceErr: err,
	}
}

func yamlSubAgentCreatorFromContextSafe(ctx context.Context) agent.AgentCreator {
	defer func() {
		_ = recover()
	}()

	return agent.SubAgentCreatorFromContext(ctx)
}

// resolveYAMLTargetPackage resolves a YAML subagent package argument to a package target. It first accepts existing directories inside opts.SandboxDir, then falls
// back to resolving target as an import path from opts.GoPkgAbsDir or the sandbox. The returned target records any known module and import metadata and whether
// the package directory is inside the sandbox; it does not enforce subagent package restrictions.
func resolveYAMLTargetPackage(opts toolsetinterface.Options, target string) (resolvedPackageTarget, error) {
	sandboxDir := opts.SandboxDir
	if strings.TrimSpace(sandboxDir) == "" {
		return resolvedPackageTarget{}, errors.New("sandbox dir is required")
	}
	absSandboxDir, err := filepath.Abs(sandboxDir)
	if err != nil {
		return resolvedPackageTarget{}, fmt.Errorf("make sandbox dir absolute: %w", err)
	}

	if absPath, relPath, err := coretools.NormalizePath(target, absSandboxDir, coretools.WantPathTypeDir, true); err == nil && relPath != "" {
		resolved := resolvedPackageTarget{
			AbsDir:        absPath,
			WithinSandbox: true,
		}
		if module, err := gocode.NewModule(absPath); err == nil {
			relDir, err := filepath.Rel(module.AbsolutePath, absPath)
			if err == nil {
				relDir = normalizeModuleRelativeDir(relDir)
				if pkg, err := module.LoadPackageByRelativeDir(relDir); err == nil {
					resolved.ImportPath = pkg.ImportPath
					resolved.ModuleAbsDir = module.AbsolutePath
					resolved.PackageRelDir = relDir
				}
			}
		}
		return resolved, nil
	}

	moduleSearchDir := opts.GoPkgAbsDir
	if strings.TrimSpace(moduleSearchDir) == "" {
		moduleSearchDir = absSandboxDir
	}

	currentModule, err := gocode.NewModule(moduleSearchDir)
	if err != nil {
		return resolvedPackageTarget{}, fmt.Errorf("resolve package %q: %w", target, err)
	}

	moduleAbsDir, packageAbsDir, packageRelDir, fqImportPath, err := currentModule.ResolvePackageByImport(target)
	if err != nil {
		return resolvedPackageTarget{}, fmt.Errorf("resolve package %q: %w", target, err)
	}

	return resolvedPackageTarget{
		AbsDir:        packageAbsDir,
		ImportPath:    fqImportPath,
		ModuleAbsDir:  moduleAbsDir,
		PackageRelDir: packageRelDir,
		WithinSandbox: clarifyPathWithinDir(absSandboxDir, packageAbsDir),
	}, nil
}

func loadCurrentPackage(opts toolsetinterface.Options) (*gocode.Package, error) {
	if strings.TrimSpace(opts.GoPkgAbsDir) == "" {
		return nil, errors.New("current package dir is required")
	}

	module, err := gocode.NewModule(opts.GoPkgAbsDir)
	if err != nil {
		return nil, err
	}

	relDir, err := filepath.Rel(module.AbsolutePath, opts.GoPkgAbsDir)
	if err != nil {
		return nil, fmt.Errorf("determine package relative dir: %w", err)
	}

	return module.LoadPackageByRelativeDir(normalizeModuleRelativeDir(relDir))
}

func loadResolvedPackage(target resolvedPackageTarget) (*gocode.Package, error) {
	if target.ModuleAbsDir == "" {
		return nil, fmt.Errorf("target package %q is not in a module", target.AbsDir)
	}

	module, err := gocode.NewModule(target.ModuleAbsDir)
	if err != nil {
		return nil, err
	}

	relDir := target.PackageRelDir
	if relDir == "." {
		relDir = ""
	}
	return module.LoadPackageByRelativeDir(relDir)
}

func yamlTargetsSamePackage(currentPkg *gocode.Package, target resolvedPackageTarget) bool {
	if currentPkg == nil {
		return false
	}
	if target.ImportPath != "" && currentPkg.ImportPath == target.ImportPath {
		return true
	}
	return currentPkg.AbsolutePath() == target.AbsDir
}
