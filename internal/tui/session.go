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
	"github.com/codalotl/codalotl/internal/agentsmd"
	"github.com/codalotl/codalotl/internal/codeunit"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/initialcontext"
	"github.com/codalotl/codalotl/internal/lints"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/prompt"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/codalotl/codalotl/internal/tools/toolsets"
)

const (
	defaultModelID = llmmodel.DefaultModel
	tuiAgentName   = "codalotl"
)

type session struct {
	agent *agent.Agent

	modelID llmmodel.ModelID

	sandboxDir string

	packagePath    string
	packageAbsPath string

	authorizer   authdomain.Authorizer
	userRequests <-chan authdomain.UserRequest

	config sessionConfig
}

type sessionConfig struct {
	packagePath string
	modelID     llmmodel.ModelID
	lintSteps   []lints.Step
	// sandboxDir, if set, overrides the default sandbox detection (os.Getwd).
	// This is primarily to make tests independent of process-wide working directory
	// and to avoid path aliasing issues (ex: /var vs /private/var on macOS).
	sandboxDir string
}

func (cfg sessionConfig) packageMode() bool {
	return strings.TrimSpace(cfg.packagePath) != ""
}

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

	sandboxAuthorizer, userRequests, err := authdomain.NewPermissiveSandboxAuthorizer(sandboxDir, nil)
	if err != nil {
		return nil, err
	}

	var tools []llmstream.Tool
	toolAuthorizer := authdomain.Authorizer(sandboxAuthorizer)

	var systemPrompt string
	if cfg.packageMode() {
		systemPrompt = prompt.GetGoPackageModeModePrompt(prompt.GoPackageModePromptKindFull)
		unitName := codeUnitName(cfg.packagePath)
		unit, err := codeunit.NewCodeUnit(unitName, pkgAbsPath)
		if err != nil {
			sandboxAuthorizer.Close()
			return nil, fmt.Errorf("build code unit: %w", err)
		}
		// Expand beyond the base package dir to include supporting data directories while
		// excluding nested Go packages. This is snapshot-based: it is computed once at
		// session creation time.
		if err := unit.IncludeSubtreeUnlessContains("*.go"); err != nil {
			sandboxAuthorizer.Close()
			return nil, fmt.Errorf("expand code unit subtree: %w", err)
		}
		if err := includeReachableTestdataDirs(unit); err != nil {
			sandboxAuthorizer.Close()
			return nil, fmt.Errorf("include reachable testdata dirs: %w", err)
		}
		pkgAuthorizer := authdomain.NewCodeUnitAuthorizer(unit, sandboxAuthorizer)
		toolAuthorizer = pkgAuthorizer
		tools, err = toolsets.PackageAgentToolsWithOptions(sandboxDir, pkgAuthorizer, pkgAbsPath, toolsets.ToolsetOptions{
			LintSteps: cfg.lintSteps,
		})
		if err != nil {
			sandboxAuthorizer.Close()
			return nil, fmt.Errorf("build package toolset: %w", err)
		}
	} else {
		systemPrompt = prompt.GetFullPrompt()
		tools, err = toolsets.CoreAgentToolsWithOptions(sandboxDir, sandboxAuthorizer, toolsets.ToolsetOptions{
			LintSteps: cfg.lintSteps,
		})
		if err != nil {
			sandboxAuthorizer.Close()
			return nil, fmt.Errorf("build toolset: %w", err)
		}
	}

	systemPrompt = strings.TrimSpace(systemPrompt)

	agentInstance, err := agent.NewAgent(modelID, systemPrompt, tools)
	if err != nil {
		sandboxAuthorizer.Close()
		return nil, fmt.Errorf("construct agent: %w", err)
	}

	envMsg := buildEnvironmentInfo(sandboxDir)
	if err := agentInstance.AddUserTurn(envMsg); err != nil {
		sandboxAuthorizer.Close()
		return nil, fmt.Errorf("add environment info: %w", err)
	}

	// In generic mode we don't gather package initialcontext, so include AGENTS.md
	// context up front if present.
	if !cfg.packageMode() {
		agentsMsg, err := agentsmd.Read(sandboxDir, sandboxDir)
		if err != nil {
			debugLogf("agentsmd.Read failed: %v", err)
		} else if strings.TrimSpace(agentsMsg) != "" {
			if err := agentInstance.AddUserTurn(agentsMsg); err != nil {
				sandboxAuthorizer.Close()
				return nil, fmt.Errorf("add AGENTS.md context: %w", err)
			}
		}
	}

	return &session{
		agent:          agentInstance,
		modelID:        modelID,
		sandboxDir:     sandboxDir,
		packagePath:    cfg.packagePath,
		packageAbsPath: pkgAbsPath,
		authorizer:     toolAuthorizer,
		userRequests:   userRequests,
		config:         cfg,
	}, nil
}

// includeReachableTestdataDirs includes any "testdata" directory directly under an
// already-included directory (recursively). This allows Go fixture files in testdata
// to remain in-scope for a package-mode code unit even when they are "*.go" files.
func includeReachableTestdataDirs(unit *codeunit.CodeUnit) error {
	if unit == nil {
		return nil
	}

	// Iterate the current snapshot of included paths; this is intentionally not a
	// filesystem walk from BaseDir() because we only want testdata under directories
	// that are already reachable per the main code unit rules.
	for _, absPath := range unit.IncludedFiles() {
		info, err := os.Stat(absPath)
		if err != nil {
			// The code unit should only contain existing paths, but be tolerant.
			continue
		}
		if !info.IsDir() {
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
		if !tdInfo.IsDir() {
			continue
		}
		if unit.Includes(testdataPath) {
			continue
		}
		if err := unit.IncludeDir(testdataPath, true); err != nil {
			return fmt.Errorf("include %q: %w", testdataPath, err)
		}
	}

	return nil
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

func (s *session) ID() string {
	if s == nil || s.agent == nil {
		return ""
	}
	return s.agent.SessionID()
}

func (s *session) SendMessage(ctx context.Context, message string) <-chan agent.Event {
	if s == nil || s.agent == nil {
		return nil
	}
	return s.agent.SendUserMessage(ctx, message)
}

func (s *session) AddGrantsFromUserMessage(message string) error {
	if s == nil || s.authorizer == nil {
		return authdomain.ErrAuthorizerCannotAcceptGrants
	}
	return authdomain.AddGrantsFromUserMessage(s.authorizer, message)
}

func (s *session) UserRequests() <-chan authdomain.UserRequest {
	return s.userRequests
}

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

// normalizeSessionConfig resolves the configured package path against the sandbox
// directory and ensures it remains inside the sandbox, returning the sanitized
// config along with the absolute package path.
func normalizeSessionConfig(cfg sessionConfig, sandboxDir string) (sessionConfig, string, error) {
	cfg.packagePath = strings.TrimSpace(cfg.packagePath)
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

func codeUnitName(pkgPath string) string {
	pkgPath = strings.TrimSpace(pkgPath)
	if pkgPath == "" || pkgPath == "." {
		return "package ."
	}
	return "package " + pkgPath
}

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

func buildPackageInitialContext(sandboxDir string, pkgRelPath string, pkgAbsPath string) (string, error) {
	agentsMsg, agentsErr := agentsmd.Read(sandboxDir, pkgAbsPath)
	if agentsErr != nil {
		debugLogf("agentsmd.Read failed: %v", agentsErr)
	}

	pkg, err := loadGoPackage(pkgAbsPath)
	if err != nil {
		return joinContextBlocks(
			agentsMsg,
			packagePathSection(pkgRelPath, pkgAbsPath, err),
		), err
	}

	pkgModeInfo, err := initialcontext.Create(pkg, nil, false)
	if err != nil {
		return joinContextBlocks(
			agentsMsg,
			packagePathSection(pkgRelPath, pkgAbsPath, err),
		), err
	}

	finalHint := fmt.Sprintf("Reminder: all file paths you send to tools **must be relative to the sandbox dir (%s)** - NOT relative to the package dir.", sandboxDir)

	// Always place AGENTS.md guidance before the rest of the generated initial context.
	return joinContextBlocks(agentsMsg, pkgModeInfo, finalHint), nil
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
