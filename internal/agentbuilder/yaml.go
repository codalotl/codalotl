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

const (
	yamlAgentModeGeneric       = "generic"
	yamlAgentModePackage       = "package"
	yamlDefaultConfigPath      = "data/config.yml"
	yamlPromptBase             = "base"
	yamlPromptPackageBase      = "package-base"
	yamlPromptLimitedPkgBase   = "limited-package-base"
	yamlToolVirtualEditFiles   = "edit_files"
	yamlRelationDirectImport   = "direct_import_of_caller"
	yamlRelationDirectImporter = "direct_importer_of_caller"
)

//go:embed data/*
var embeddedYAMLData embed.FS

type yamlRegistrySpec struct {
	Agents []yamlAgentSpec `yaml:"agents"`
	Tools  []yamlToolSpec  `yaml:"tools"`
}

type yamlAgentSpec struct {
	Name                      string          `yaml:"name"`
	Prompts                   []yamlPromptRef `yaml:"prompts"`
	Tools                     []string        `yaml:"tools"`
	Mode                      string          `yaml:"mode"`
	IncludePackageModeContext bool            `yaml:"include_package_mode_context"`
	Skills                    *bool           `yaml:"skills"`
}

type yamlPromptRef struct {
	Name string `yaml:"name"`
	File string `yaml:"file"`
	Text string `yaml:"text"`
}

type yamlToolSpec struct {
	Name        string                       `yaml:"name"`
	Description string                       `yaml:"description"`
	Parameters  map[string]yamlToolParameter `yaml:"parameters"`
	Command     *yamlCommandSpec             `yaml:"command"`
	Subagent    *yamlSubagentSpec            `yaml:"subagent"`
}

type yamlToolParameter struct {
	Type        string `yaml:"type"`
	Description string `yaml:"description"`
	Required    bool   `yaml:"required"`
}

type yamlCommandSpec struct {
	Cmd  string   `yaml:"cmd"`
	Args []string `yaml:"args"`
	CWD  string   `yaml:"cwd"`
}

type yamlSubagentSpec struct {
	Name                string                           `yaml:"name"`
	Package             string                           `yaml:"package"`
	Message             string                           `yaml:"message"`
	PackageRestrictions *yamlSubagentPackageRestrictions `yaml:"package_restrictions"`
}

type yamlSubagentPackageRestrictions struct {
	DisallowSelf        bool   `yaml:"disallow_self"`
	Relation            string `yaml:"relation"`
	AllowOutsideSandbox bool   `yaml:"allow_outside_sandbox"`
	RequirePackageMode  bool   `yaml:"require_package_mode"`
}

type yamlNormalizedToolSpec struct {
	Name              string
	Description       string
	Parameters        map[string]yamlNormalizedParameter
	Command           *yamlCommandSpec
	Subagent          *yamlSubagentSpec
	TargetPackageMode bool
}

type yamlNormalizedParameter struct {
	Type        string
	Description string
	Required    bool
}

type yamlPreparedAgent struct {
	Definition agentregistry.Definition
}

type yamlCommandTool struct {
	info   llmstream.ToolInfo
	spec   *yamlCommandSpec
	params map[string]yamlNormalizedParameter
	opts   toolsetinterface.Options
}

type yamlSubagentTool struct {
	info              llmstream.ToolInfo
	spec              *yamlSubagentSpec
	params            map[string]yamlNormalizedParameter
	opts              toolsetinterface.Options
	targetPackageMode bool
}

type resolvedPackageTarget struct {
	AbsDir        string
	ImportPath    string
	ModuleAbsDir  string
	PackageRelDir string
	WithinSandbox bool
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
		normalized, err := normalizeYAMLToolSpec(toolSpec, existingAgentModes, newAgentModes)
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

func normalizeYAMLToolSpec(spec yamlToolSpec, existingAgentModes map[string]string, newAgentModes map[string]string) (yamlNormalizedToolSpec, error) {
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
			Command:     spec.Command,
		}, nil
	}

	if strings.TrimSpace(spec.Subagent.Name) == "" {
		return yamlNormalizedToolSpec{}, errors.New("subagent.name is required")
	}
	if strings.TrimSpace(spec.Subagent.Message) == "" {
		return yamlNormalizedToolSpec{}, errors.New("subagent.message is required")
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

	return yamlNormalizedToolSpec{
		Name:              spec.Name,
		Description:       spec.Description,
		Parameters:        normalizedParams,
		Subagent:          spec.Subagent,
		TargetPackageMode: targetMode == yamlAgentModePackage,
	}, nil
}

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

	prepared := yamlPreparedAgent{}
	prepared.Definition = agentregistry.Definition{
		Name:        spec.Name,
		Description: "YAML-defined " + spec.Mode + " agent.",
		ToolsBuilder: func(opts toolsetinterface.Options) ([]string, error) {
			return expandYAMLToolNames(spec.Tools, opts.Model), nil
		},
		SystemPromptBuilder: func(options agentregistry.BuildOptions) (string, error) {
			return buildYAMLAgentSystemPrompt(options, resolvedPrompt, enableSkills, spec.Mode == yamlAgentModePackage, spec.Tools)
		},
	}
	if spec.Mode == yamlAgentModePackage {
		prepared.Definition.AuthPolicy = agentregistry.AuthPolicyPackage
	}
	if spec.IncludePackageModeContext {
		prepared.Definition.InitialTurnsBuilder = buildPackageModeDefaultContextInitialTurns
	}
	if err := prepared.Definition.Validate(); err != nil {
		return yamlPreparedAgent{}, err
	}

	return prepared, nil
}

func resolveYAMLAgentPrompt(yamlFS fs.FS, yamlDir string, prompts []yamlPromptRef) (string, error) {
	blocks := make([]string, 0, len(prompts))
	for i, promptRef := range prompts {
		count := 0
		if promptRef.Name != "" {
			count++
		}
		if promptRef.File != "" {
			count++
		}
		if promptRef.Text != "" {
			count++
		}
		if count != 1 {
			return "", fmt.Errorf("prompt %d must set exactly one of name, file, or text", i)
		}

		switch {
		case promptRef.Name != "":
			text, err := resolveBuiltinYAMLPrompt(promptRef.Name)
			if err != nil {
				return "", err
			}
			blocks = append(blocks, text)
		case promptRef.File != "":
			promptPath := filepath.Join(yamlDir, promptRef.File)
			content, err := fs.ReadFile(yamlFS, promptPath)
			if err != nil {
				return "", fmt.Errorf("read prompt file %q: %w", promptRef.File, err)
			}
			blocks = append(blocks, string(content))
		case promptRef.Text != "":
			blocks = append(blocks, promptRef.Text)
		}
	}

	return joinContextBlocks(blocks...), nil
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
			return nil, fmt.Errorf("tool %q is not registered", name)
		}
	}
	return resolved, nil
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

		if spec.Command != nil {
			return &yamlCommandTool{
				info:   info,
				spec:   spec.Command,
				params: spec.Parameters,
				opts:   opts,
			}, nil
		}

		return &yamlSubagentTool{
			info:              info,
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

func (t *yamlCommandTool) Info() llmstream.ToolInfo {
	return t.info
}

func (t *yamlCommandTool) Name() string {
	return t.info.Name
}

func (t *yamlCommandTool) Run(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
	params, err := parseYAMLToolCallParams(call.Input, t.params)
	if err != nil {
		return yamlToolErrorResult(call, err)
	}

	data, err := buildYAMLTemplateData(t.opts, params)
	if err != nil {
		return yamlToolErrorResult(call, err)
	}

	renderedCmd, err := renderYAMLTemplateString(t.spec.Cmd, data)
	if err != nil {
		return yamlToolErrorResult(call, fmt.Errorf("render command.cmd: %w", err))
	}

	renderedArgs := make([]string, 0, len(t.spec.Args))
	for i, arg := range t.spec.Args {
		renderedArg, err := renderYAMLTemplateString(arg, data)
		if err != nil {
			return yamlToolErrorResult(call, fmt.Errorf("render command.args[%d]: %w", i, err))
		}
		renderedArgs = append(renderedArgs, renderedArg)
	}

	cwd := t.opts.SandboxDir
	if strings.TrimSpace(cwd) == "" {
		cwd, err = os.Getwd()
		if err != nil {
			return yamlToolErrorResult(call, fmt.Errorf("determine working directory: %w", err))
		}
	}
	if t.spec.CWD != "" {
		cwd, err = renderYAMLTemplateString(t.spec.CWD, data)
		if err != nil {
			return yamlToolErrorResult(call, fmt.Errorf("render command.cwd: %w", err))
		}
	}

	command := append([]string{renderedCmd}, renderedArgs...)
	if t.opts.Authorizer != nil {
		if err := t.opts.Authorizer.IsShellAuthorized(false, "", cwd, command); err != nil {
			return yamlToolErrorResult(call, err)
		}
	}

	runner := cmdrunner.NewRunner(nil, nil)
	runner.AddCommand(cmdrunner.Command{
		Command: renderedCmd,
		Args:    renderedArgs,
		CWD:     cwd,
		ShowCWD: true,
	})

	rootDir := t.opts.SandboxDir
	if strings.TrimSpace(rootDir) == "" {
		rootDir = cwd
	}
	result, err := runner.Run(ctx, rootDir, nil)
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

func (t *yamlSubagentTool) Info() llmstream.ToolInfo {
	return t.info
}

func (t *yamlSubagentTool) Name() string {
	return t.info.Name
}

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

	message, err := renderYAMLTemplateString(t.spec.Message, templateData)
	if err != nil {
		return yamlToolErrorResult(call, fmt.Errorf("render subagent.message: %w", err))
	}

	subAgentCreator := yamlSubAgentCreatorFromContextSafe(ctx)
	req, err := t.buildInvokeRequest(message, params, subAgentCreator)
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

	return llmstream.ToolResult{
		CallID: call.CallID,
		Name:   call.Name,
		Type:   call.Type,
		Result: answer,
	}
}

func (t *yamlSubagentTool) buildInvokeRequest(message string, params map[string]any, agentCreator agent.AgentCreator) (toolsetinterface.InvokeRequest, error) {
	req := toolsetinterface.InvokeRequest{
		Messages: []string{message},
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
		req.CallerAuthorizer = t.buildTargetPackageAuthorizer(targetPackage, overrideSandboxDir)
		req.ToolOptions.Authorizer = req.CallerAuthorizer
	}

	return req, nil
}

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

func (t *yamlSubagentTool) buildTargetPackageAuthorizer(target resolvedPackageTarget, sandboxDir string) authdomain.Authorizer {
	fallback := authdomain.NewAutoApproveAuthorizer(sandboxDir)
	if target.WithinSandbox && t.opts.Authorizer != nil {
		fallback = t.opts.Authorizer.WithoutCodeUnit()
	}

	unit, err := codeunit.NewCodeUnit("package "+target.AbsDir, target.AbsDir)
	if err != nil {
		return fallback
	}
	if err := unit.IncludeSubtreeUnlessContains("*.go"); err == nil {
		_ = includeReachableTestdataDirs(unit)
		unit.PruneEmptyDirs()
	}
	return authdomain.NewCodeUnitAuthorizer(unit, fallback)
}

func includeReachableTestdataDirs(unit *codeunit.CodeUnit) error {
	if unit == nil {
		return nil
	}

	for _, absPath := range unit.IncludedFiles() {
		info, err := os.Stat(absPath)
		if err != nil || !info.IsDir() {
			continue
		}

		testdataPath := filepath.Join(absPath, "testdata")
		tdInfo, err := os.Stat(testdataPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("stat %q: %w", testdataPath, err)
		}
		if !tdInfo.IsDir() || unit.Includes(testdataPath) {
			continue
		}
		if err := unit.IncludeDir(testdataPath, true); err != nil {
			return fmt.Errorf("include %q: %w", testdataPath, err)
		}
	}

	return nil
}

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
