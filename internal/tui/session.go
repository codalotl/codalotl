package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/agentbuilder"
	"github.com/codalotl/codalotl/internal/codeunit"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/initialcontext"
	"github.com/codalotl/codalotl/internal/lints"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/prompt"
	"github.com/codalotl/codalotl/internal/skills"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/codalotl/codalotl/internal/tools/toolsetinterface"
)

// Session defaults and agent names used by the TUI.
const (
	defaultModelID       = llmmodel.DefaultModel // defaultModelID is the fallback model for sessions without an explicit model.
	tuiAgentName         = "codalotl"            // tuiAgentName is the canonical name for the main Codalotl TUI agent.
	orchestrateAgentName = "pr-orchestrator"     // orchestrateAgentName is the registry name for the built-in PR orchestrator agent.
)

var newRootAgentCreator = agent.NewAgentCreator

// session represents one active TUI agent session and its associated resources.
type session struct {
	agent            *agent.Agent       // agent runs the conversation loop for this session.
	queueUserMessage func(string) error // queueUserMessage queues user text for the next safe agent boundary during an active run.
	modelID          llmmodel.ModelID   // modelID is the selected LLM model for this session.
	sandboxDir       string             // sandboxDir is the normalized sandbox root used for authorization, tools, and skill discovery.
	packagePath      string             // packagePath is the sandbox-relative package path for Package Mode, or empty outside Package Mode.
	packageAbsPath   string             // packageAbsPath is the absolute package path for Package Mode, or empty outside Package Mode.
	availableSkills  []skills.Skill     // availableSkills are valid skills discovered for this session and shown by /skills.
	invalidSkills    []skills.Skill     // invalidSkills are discovered skills with validation problems shown by /skills.

	// failedSkillLoads are errors returned by skills.LoadSkill when attempting to load a candidate skill dir (typically due to missing/invalid SKILL.md, IO errors,
	// etc). skillsLoadErr is a fatal skills discovery error (rare); both are surfaced to the user via `/skills`.
	failedSkillLoads []error

	skillsLoadErr error                         // skillsLoadErr is a skills discovery error that is non-fatal to session startup and shown by /skills.
	authorizer    authdomain.Authorizer         // authorizer mediates tool permissions for this session and must be closed when the session ends.
	userRequests  <-chan authdomain.UserRequest // userRequests receives permission prompts emitted by the session authorizer.
	config        sessionConfig                 // config is the normalized configuration used to construct this session.
}

// sessionConfig configures construction and reset of a TUI agent session.
type sessionConfig struct {
	packagePath string           // Package path selects Package Mode when non-blank and is interpreted relative to the sandbox.
	agentName   string           // Agent name selects a specialized non-package agent; Package Mode overrides it.
	modelID     llmmodel.ModelID // Model ID selects the LLM model; an empty value uses the default model.
	lintSteps   []lints.Step     // Lint steps configure package checks used by tools and package-context gathering.
	autoYes     bool             // Auto yes approves permission requests through the session authorizer.

	// sandboxDir, if set, overrides the default sandbox detection (os.Getwd). This is primarily to make tests independent of process-wide working directory and to avoid
	// path aliasing issues (ex: /var vs /private/var on macOS).
	sandboxDir string
}

// packageMode reports whether cfg selects Package Mode.
func (cfg sessionConfig) packageMode() bool {
	return strings.TrimSpace(cfg.packagePath) != ""
}

// orchestrateMode reports whether cfg selects the built-in orchestrator agent.
func (cfg sessionConfig) orchestrateMode() bool {
	return strings.TrimSpace(cfg.agentName) == orchestrateAgentName
}

// newSession constructs an agent session from cfg, including the authorizer, tools, skills, selected agent, and initial environment turn. It normalizes the sandbox
// and package path, uses the default model when cfg.modelID is empty, and applies Package Mode code-unit restrictions when cfg.packagePath is set. The caller must
// close the returned session when it is replaced or no longer needed.
func newSession(cfg sessionConfig) (*session, error) {
	sandboxDir := strings.TrimSpace(cfg.sandboxDir)
	if sandboxDir == "" {
		var err error
		sandboxDir, err = determineSandboxDir()
		if err != nil {
			return nil, err
		}
	}

	cfg, pkgAbsPath, err := normalizeSessionConfig(cfg, sandboxDir)
	if err != nil {
		return nil, err
	}
	cfg.sandboxDir = sandboxDir

	modelID := cfg.modelID
	if modelID == "" {
		modelID = defaultModelID
	}
	if modelID != "" && !modelID.Valid() {
		return nil, fmt.Errorf("unknown model %q", modelID)
	}
	prompt.SetModel(modelID)

	sandboxAuthorizer, userRequests, err := authdomain.NewSessionAuthorizer(sandboxDir, nil, cfg.autoYes)
	if err != nil {
		return nil, err
	}

	skillSearchStartDir := sandboxDir
	if cfg.packageMode() {
		skillSearchStartDir = pkgAbsPath
	}
	installErr := skills.InstallDefault()
	if installErr != nil {
		sandboxAuthorizer.Close()
		return nil, fmt.Errorf("install default skills: %w", installErr)
	}

	searchDirs := skills.SearchPaths(skillSearchStartDir)
	validSkills, invalidSkills, failedSkillLoads, skillsLoadErr := skills.LoadSkills(searchDirs)
	if skillsLoadErr != nil {
		// Non-fatal: session startup should still succeed even if `/skills` cannot show results.
		debugLogf("skills.LoadSkills failed: %v", skillsLoadErr)
		validSkills = nil
		invalidSkills = nil
		failedSkillLoads = nil
	}
	if len(invalidSkills) > 0 || len(failedSkillLoads) > 0 {
		debugLogf("skills issues:\n%s", skills.FormatSkillErrors(invalidSkills, failedSkillLoads))
	}

	toolAuthorizer := authdomain.Authorizer(sandboxAuthorizer)
	toolOptions := toolsetinterface.Options{
		SandboxDir: sandboxDir,
		Authorizer: toolAuthorizer,
		Model:      modelID,
		LintSteps:  cfg.lintSteps,
	}
	agentName := agentbuilder.AgentGeneric
	if cfg.orchestrateMode() {
		agentName = orchestrateAgentName
	}
	if cfg.packageMode() {
		unit, err := codeunit.DefaultGoCodeUnit(pkgAbsPath)
		if err != nil {
			sandboxAuthorizer.Close()
			return nil, fmt.Errorf("build code unit: %w", err)
		}
		pkgAuthorizer := authdomain.NewCodeUnitAuthorizer(unit, sandboxAuthorizer)
		toolAuthorizer = pkgAuthorizer
		toolOptions.Authorizer = pkgAuthorizer
		toolOptions.GoPkgAbsDir = pkgAbsPath
		agentName = agentbuilder.AgentPackageModeNoContext
	}

	registry, err := agentbuilder.BuildRegistry()
	if err != nil {
		sandboxAuthorizer.Close()
		return nil, fmt.Errorf("build agent registry: %w", err)
	}

	prepared, err := registry.Prepare(context.Background(), agentName, toolsetinterface.InvokeRequest{
		ToolOptions: toolOptions,
	})
	if err != nil {
		sandboxAuthorizer.Close()
		return nil, fmt.Errorf("prepare agent: %w", err)
	}
	prepared.InitialTurns = append(prepared.InitialTurns, buildEnvironmentInfo(sandboxDir))

	agentInstance, err := prepared.Create(newSessionAgentCreator())
	if err != nil {
		sandboxAuthorizer.Close()
		return nil, fmt.Errorf("construct agent: %w", err)
	}

	return &session{
		agent:            agentInstance,
		queueUserMessage: agentInstance.QueueUserMessage,
		modelID:          modelID,
		sandboxDir:       sandboxDir,
		packagePath:      cfg.packagePath,
		packageAbsPath:   pkgAbsPath,
		availableSkills:  validSkills,
		invalidSkills:    invalidSkills,
		failedSkillLoads: failedSkillLoads,
		skillsLoadErr:    skillsLoadErr,
		authorizer:       toolAuthorizer,
		userRequests:     userRequests,
		config:           cfg,
	}, nil
}

func newSessionAgentCreator() agent.AgentCreator {
	if os.Getenv("CODALOTL_ZDR") == "true" {
		return newRootAgentCreator(agent.NewOptions{NoStore: true})
	}
	return newRootAgentCreator()
}

// Close releases resources acquired for the session, notably the sandbox authorizer.
func (s *session) Close() {
	if s == nil {
		return
	}
	if s.authorizer != nil {
		s.authorizer.Close()
	}
}

// ID returns the agent session ID, or an empty string if no agent is available.
func (s *session) ID() string {
	if s == nil || s.agent == nil {
		return ""
	}
	return s.agent.SessionID()
}

// SendMessage sends message to the agent and returns the resulting event stream.
func (s *session) SendMessage(ctx context.Context, message string) <-chan agent.Event {
	if s == nil || s.agent == nil {
		return nil
	}
	return s.agent.SendUserMessage(ctx, message)
}

// QueueUserMessage queues message for delivery at the agent's next safe boundary.
func (s *session) QueueUserMessage(message string) error {
	if s == nil {
		return agent.ErrNotRunning
	}
	if s.queueUserMessage != nil {
		return s.queueUserMessage(message)
	}
	if s.agent == nil {
		return agent.ErrNotRunning
	}
	return s.agent.QueueUserMessage(message)
}

// AddGrantsFromUserMessage applies authorization grants found in message to the session authorizer.
func (s *session) AddGrantsFromUserMessage(message string) error {
	if s == nil || s.authorizer == nil {
		return authdomain.ErrAuthorizerCannotAcceptGrants
	}
	return authdomain.AddGrantsFromUserMessage(s.authorizer, message)
}

// UserRequests returns the permission request channel for this session.
func (s *session) UserRequests() <-chan authdomain.UserRequest {
	return s.userRequests
}

// ModelName returns the selected model name, or the default model name if none is set.
func (s *session) ModelName() string {
	if s == nil {
		return string(defaultModelID)
	}
	if s.modelID != "" {
		return string(s.modelID)
	}
	return string(defaultModelID)
}

func determineSandboxDir() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Clean(cwd), nil
}

func buildEnvironmentInfo(sandboxDir string) string {
	isGit := isGitRepo(sandboxDir)
	date := time.Now().Format("1/2/2006")
	return fmt.Sprintf(`Here is useful information about the environment you are running in:
<env>
Sandbox directory: %s
Is directory a git repo: %s
Platform: %s
Today's date: %s
</env>
`, sandboxDir, boolToYesNo(isGit), runtime.GOOS, date)
}

func isGitRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

func boolToYesNo(v bool) string {
	if v {
		return "Yes"
	}
	return "No"
}

// normalizeSessionConfig resolves the configured package path against the sandbox directory and ensures it remains inside the sandbox, returning the sanitized config
// along with the absolute package path.
func normalizeSessionConfig(cfg sessionConfig, sandboxDir string) (sessionConfig, string, error) {
	cfg.packagePath = strings.TrimSpace(cfg.packagePath)
	cfg.agentName = strings.TrimSpace(cfg.agentName)
	cfg.modelID = llmmodel.ModelID(strings.TrimSpace(string(cfg.modelID)))
	if cfg.modelID == "" {
		cfg.modelID = defaultModelID
	}
	if !cfg.packageMode() {
		return cfg, "", nil
	}

	sandboxDir = filepath.Clean(sandboxDir)
	if !filepath.IsAbs(sandboxDir) {
		return cfg, "", fmt.Errorf("sandbox dir must be absolute")
	}

	pkgPath := cfg.packagePath
	if filepath.IsAbs(pkgPath) {
		relToSandbox, err := filepath.Rel(sandboxDir, filepath.Clean(pkgPath))
		if err != nil {
			return cfg, "", fmt.Errorf("normalize package path: %w", err)
		}
		pkgPath = relToSandbox
	}

	if pkgPath == "" {
		pkgPath = "."
	}

	absPkgPath := filepath.Clean(filepath.Join(sandboxDir, pkgPath))
	relToSandbox, err := filepath.Rel(sandboxDir, absPkgPath)
	if err != nil {
		return cfg, "", fmt.Errorf("normalize package path: %w", err)
	}
	if relToSandbox == ".." || strings.HasPrefix(relToSandbox, ".."+string(filepath.Separator)) {
		return cfg, "", fmt.Errorf("package path %q is outside the sandbox %q", cfg.packagePath, sandboxDir)
	}

	info, err := os.Stat(absPkgPath)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, "", fmt.Errorf("package path %q does not exist", cfg.packagePath)
		}
		return cfg, "", fmt.Errorf("stat package path %q: %w", cfg.packagePath, err)
	}
	if !info.IsDir() {
		return cfg, "", fmt.Errorf("package path %q is not a directory", cfg.packagePath)
	}

	relToSandbox = filepath.ToSlash(relToSandbox)
	if relToSandbox == "" {
		relToSandbox = "."
	}
	cfg.packagePath = relToSandbox

	return cfg, absPkgPath, nil
}

// loadGoPackage loads the Go package at pkgAbsPath from its enclosing module.
func loadGoPackage(pkgAbsPath string) (*gocode.Package, error) {
	if pkgAbsPath == "" {
		return nil, fmt.Errorf("empty package path")
	}
	module, err := gocode.NewModule(pkgAbsPath)
	if err != nil {
		return nil, fmt.Errorf("load module: %w", err)
	}

	relDir, err := filepath.Rel(module.AbsolutePath, pkgAbsPath)
	if err != nil {
		return nil, fmt.Errorf("resolve package dir: %w", err)
	}
	if relDir == "." {
		relDir = ""
	}

	pkg, err := module.LoadPackageByRelativeDir(relDir)
	if err != nil {
		return nil, fmt.Errorf("load package: %w", err)
	}

	return pkg, nil
}

func buildPackageInitialContext(sandboxDir string, pkgRelPath string, pkgAbsPath string, lintSteps []lints.Step) (string, error) {
	pkg, err := loadGoPackage(pkgAbsPath)
	if err != nil {
		return joinContextBlocks(packagePathSection(pkgRelPath, pkgAbsPath, err)), err
	}

	pkgModeInfo, err := initialcontext.Create(pkg, lintSteps, false)
	if err != nil {
		return joinContextBlocks(packagePathSection(pkgRelPath, pkgAbsPath, err)), err
	}

	finalHint := fmt.Sprintf("Reminder: all file paths you send to tools **must be relative to the sandbox dir (%s)** - NOT relative to the package dir.", sandboxDir)

	return joinContextBlocks(pkgModeInfo, finalHint), nil
}

func packagePathSection(pkgRelPath string, pkgAbsPath string, err error) string {
	var b strings.Builder
	b.WriteString("<package-mode>\n")
	fmt.Fprintf(&b, "Package relative path: %q\n", pkgRelPath)
	fmt.Fprintf(&b, "Package absolute path: %q\n", pkgAbsPath)
	if err != nil {
		fmt.Fprintf(&b, "Package details unavailable: %v\n", err)
	}
	b.WriteString("</package-mode>")
	return b.String()
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
