package refactor

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

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/codeunit"
	"github.com/codalotl/codalotl/internal/gocas"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/lints"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/q/cas"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	toolcli "github.com/codalotl/codalotl/internal/tools/cli"
	"github.com/codalotl/codalotl/internal/tools/toolsetinterface"
)

// ToolNameRefactor is the refactor tool name.
const ToolNameRefactor = "refactor"

// Params are the refactor tool parameters.
type Params struct {
	Name    string `json:"name"`
	Package string `json:"package"`
}

// ResultStatus describes the outcome of a refactor run.
type ResultStatus string

const (
	ResultStatusApplied        ResultStatus = "applied"
	ResultStatusNoOpportunity  ResultStatus = "no_opportunity"
	ResultStatusAlreadyApplied ResultStatus = "already_applied"
)

// Result is the machine-readable refactor tool result.
type Result struct {
	Name           string       `json:"name"`
	Package        string       `json:"package"`
	Status         ResultStatus `json:"status"`
	Message        string       `json:"message,omitempty"`
	EditedFiles    []string     `json:"edited-files"`
	SavedCASRecord *string      `json:"saved-cas-record"`
}

// Options configures the refactor tool.
type Options struct {
	AgentInvoker   toolsetinterface.AgentInvoker
	Model          llmmodel.ModelID
	LintSteps      []lints.Step
	NewCommandTree toolcli.CommandTreeFunc
}

//go:embed data/*.md
var promptFS embed.FS

type casPolicy string

const (
	casPolicyIgnore   casPolicy = "cas-ignore"
	casPolicyCodeUnit casPolicy = "cas-code-unit"
)

type refactorKind string

const (
	refactorKindDocsAdd refactorKind = "docs-add"
	refactorKindPrompt  refactorKind = "prompt"
)

type refactorConfig struct {
	name        string
	description string
	kind        refactorKind
	casPolicy   casPolicy
	promptPath  string
	agentName   string
	generation  int
}

var refactorRegistry = []refactorConfig{
	{
		name:        "docs-add",
		description: "Add missing public Go documentation with codalotl docs add.",
		kind:        refactorKindDocsAdd,
		casPolicy:   casPolicyIgnore,
	},
	{
		name:        "dry",
		description: "Share helpers and combine similar helper logic within a package.",
		kind:        refactorKindPrompt,
		casPolicy:   casPolicyCodeUnit,
		promptPath:  "data/dry.md",
		agentName:   "limited_package_mode",
		generation:  1,
	},
}

type refactorTool struct {
	authorizer authdomain.Authorizer
	options    Options
	registry   map[string]refactorConfig
}

// NewRefactorTool creates the refactor tool.
func NewRefactorTool(authorizer authdomain.Authorizer, options Options) llmstream.Tool {
	registry := make(map[string]refactorConfig, len(refactorRegistry))
	for _, cfg := range refactorRegistry {
		registry[cfg.name] = cfg
	}
	return refactorTool{
		authorizer: authorizer,
		options:    options,
		registry:   registry,
	}
}

func (t refactorTool) Info() llmstream.ToolInfo {
	return llmstream.ToolInfo{
		Name:        ToolNameRefactor,
		Description: t.description(),
		Parameters: map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "Refactor name.",
			},
			"package": map[string]any{
				"type":        "string",
				"description": "Go package directory, current-module import path, or current-module relative package path.",
			},
		},
		Required: []string{"name", "package"},
	}
}

func (t refactorTool) Name() string {
	return ToolNameRefactor
}

func (t refactorTool) Presenter() llmstream.Presenter {
	return refactorPresenter{}
}

func (t refactorTool) Run(ctx context.Context, toolCall llmstream.ToolCall) llmstream.ToolResult {
	params, err := parseParams(toolCall.Input)
	if err != nil {
		return errorToolResult(toolCall, err)
	}

	cfg, ok := t.registry[params.Name]
	if !ok {
		return errorToolResult(toolCall, fmt.Errorf("unknown refactor name %q", params.Name))
	}

	resolved, err := resolvePackage(t.authorizer, params.Package)
	if err != nil {
		return errorToolResult(toolCall, err)
	}

	var result Result
	switch cfg.kind {
	case refactorKindDocsAdd:
		result, err = t.runDocsAdd(ctx, resolved, cfg)
	case refactorKindPrompt:
		result, err = t.runPromptRefactor(ctx, resolved, cfg)
	default:
		err = fmt.Errorf("unsupported refactor kind %q", cfg.kind)
	}
	if err != nil {
		return errorToolResult(toolCall, err)
	}

	payload, err := json.Marshal(result)
	if err != nil {
		return errorToolResult(toolCall, err)
	}
	return llmstream.ToolResult{
		CallID: toolCall.CallID,
		Name:   ToolNameRefactor,
		Type:   toolCall.Type,
		Result: string(payload),
	}
}

func (t refactorTool) description() string {
	var b strings.Builder
	b.WriteString("Apply a package-local canned refactor. Available refactors:\n")
	for _, cfg := range refactorRegistry {
		fmt.Fprintf(&b, "- %s: %s\n", cfg.name, cfg.description)
	}
	b.WriteString("Package must be in the current module and inside the sandbox.")
	return b.String()
}

func (t refactorTool) runDocsAdd(ctx context.Context, resolved resolvedPackage, cfg refactorConfig) (Result, error) {
	if cfg.casPolicy != casPolicyIgnore {
		return Result{}, fmt.Errorf("docs-add refactor requires CAS policy %q", casPolicyIgnore)
	}
	if t.options.NewCommandTree == nil {
		return Result{}, errors.New("docs-add refactor requires NewCommandTree")
	}

	beforeUnit, err := codeunit.DefaultGoCodeUnit(resolved.absDir)
	if err != nil {
		return Result{}, err
	}
	beforeFiles, err := snapshotCodeUnitFiles(resolved.absDir, beforeUnit)
	if err != nil {
		return Result{}, err
	}

	cliTool := toolcli.NewCodalotlCLITool(t.options.NewCommandTree)
	cliParams := toolcli.Params{
		Subcommand: "docs",
		Argv:       []string{"add", "--public-only", resolved.absDir},
	}
	input, err := json.Marshal(cliParams)
	if err != nil {
		return Result{}, err
	}

	cliResult := cliTool.Run(ctx, llmstream.ToolCall{
		CallID: "refactor-docs-add",
		Name:   toolcli.ToolNameCodalotlCLI,
		Type:   "function_call",
		Input:  string(input),
	})
	if cliResult.IsError {
		return Result{}, errors.New(cliResult.Result)
	}

	var parsed toolcli.Result
	if err := json.Unmarshal([]byte(cliResult.Result), &parsed); err != nil {
		return Result{}, err
	}
	if !parsed.Success {
		msg := "codalotl docs add failed"
		if parsed.Stderr != "" {
			msg = parsed.Stderr
		} else if parsed.Stdout != "" {
			msg = parsed.Stdout
		}
		return Result{}, errors.New(msg)
	}

	afterUnit, err := codeunit.DefaultGoCodeUnit(resolved.absDir)
	if err != nil {
		return Result{}, err
	}
	afterFiles, err := snapshotCodeUnitFiles(resolved.absDir, afterUnit)
	if err != nil {
		return Result{}, err
	}
	edited := changedFiles(beforeFiles, afterFiles)

	status := ResultStatusApplied
	message := "successfully applied refactor"
	if docsAddNoOpportunity(parsed.Stdout) {
		status = ResultStatusNoOpportunity
		message = "no refactoring opportunities found"
	}
	return Result{
		Name:           cfg.name,
		Package:        resolved.relDir,
		Status:         status,
		Message:        message,
		EditedFiles:    edited,
		SavedCASRecord: nil,
	}, nil
}

func docsAddNoOpportunity(stdout string) bool {
	return strings.Contains(stdout, "Nothing left to document!") &&
		!strings.Contains(stdout, "Applied ")
}

func (t refactorTool) runPromptRefactor(ctx context.Context, resolved resolvedPackage, cfg refactorConfig) (Result, error) {
	if t.options.AgentInvoker == nil {
		return Result{}, errors.New("prompt-style refactor requires AgentInvoker")
	}
	if cfg.casPolicy != casPolicyCodeUnit {
		return Result{}, fmt.Errorf("unsupported CAS policy %q", cfg.casPolicy)
	}

	beforeUnit, err := codeunit.DefaultGoCodeUnit(resolved.absDir)
	if err != nil {
		return Result{}, err
	}
	db, err := t.newCASDB(resolved)
	if err != nil {
		return Result{}, err
	}
	namespace := cfg.casNamespace()
	var casRecord refactorCASRecord
	ok, _, err := db.RetrieveOnCodeUnit(beforeUnit, namespace, &casRecord)
	if err != nil {
		return Result{}, err
	}
	if ok {
		// Only CAS-backed refactors report already_applied.
		return Result{
			Name:           cfg.name,
			Package:        resolved.relDir,
			Status:         ResultStatusAlreadyApplied,
			Message:        "refactor already applied",
			EditedFiles:    []string{},
			SavedCASRecord: nil,
		}, nil
	}

	beforeFiles, err := snapshotCodeUnitFiles(resolved.absDir, beforeUnit)
	if err != nil {
		return Result{}, err
	}

	prompt, err := loadPrompt(cfg, resolved)
	if err != nil {
		return Result{}, err
	}
	if err := t.invokePromptAgent(ctx, resolved, cfg, prompt, beforeUnit); err != nil {
		return Result{}, err
	}

	afterUnit, err := codeunit.DefaultGoCodeUnit(resolved.absDir)
	if err != nil {
		return Result{}, err
	}
	afterFiles, err := snapshotCodeUnitFiles(resolved.absDir, afterUnit)
	if err != nil {
		return Result{}, err
	}
	edited := changedFiles(beforeFiles, afterFiles)
	casRecordAbsPath, err := codeUnitCASRecordPath(db, afterUnit, namespace)
	if err != nil {
		return Result{}, err
	}
	if err := db.StoreOnCodeUnit(afterUnit, namespace, refactorCASRecord{Applied: true, Edited: edited}); err != nil {
		return Result{}, err
	}
	savedCASRecord := resultPath(resolved.moduleAbsDir, casRecordAbsPath)

	status := ResultStatusApplied
	message := "successfully applied refactor"
	if len(edited) == 0 {
		status = ResultStatusNoOpportunity
		message = "no refactoring opportunities found"
	}
	return Result{
		Name:           cfg.name,
		Package:        resolved.relDir,
		Status:         status,
		Message:        message,
		EditedFiles:    edited,
		SavedCASRecord: &savedCASRecord,
	}, nil
}

func (t refactorTool) invokePromptAgent(ctx context.Context, resolved resolvedPackage, cfg refactorConfig, prompt string, unit *codeunit.CodeUnit) error {
	pkgAuthorizer := authdomain.NewCodeUnitAuthorizer(unit, t.authorizer.WithoutCodeUnit())
	sandboxDir := t.authorizer.SandboxDir()
	events, err := t.options.AgentInvoker.Invoke(ctx, cfg.agentName, toolsetinterface.InvokeRequest{
		ToolOptions: toolsetinterface.Options{
			AgentName:    cfg.agentName,
			SandboxDir:   sandboxDir,
			Authorizer:   pkgAuthorizer,
			GoPkgAbsDir:  resolved.absDir,
			Model:        t.options.Model,
			LintSteps:    t.options.LintSteps,
			AgentInvoker: t.options.AgentInvoker,
		},
		AgentCreator:     agentCreatorFromContext(ctx),
		CallerAuthorizer: pkgAuthorizer,
		CallerSandboxDir: sandboxDir,
		Messages:         []string{prompt},
	})
	if err != nil {
		return err
	}

	terminal := false
	for event := range events {
		switch event.Type {
		case agent.EventTypeError:
			if event.Error != nil {
				return event.Error
			}
			return errors.New("prompt refactor agent failed")
		case agent.EventTypeCanceled:
			if event.Error != nil {
				return event.Error
			}
			return context.Canceled
		case agent.EventTypeDoneSuccess:
			terminal = true
		}
	}
	if !terminal {
		return errors.New("prompt refactor agent ended without success")
	}
	return nil
}

func agentCreatorFromContext(ctx context.Context) (creator agent.AgentCreator) {
	defer func() {
		if recover() != nil {
			creator = agent.NewAgentCreator()
		}
	}()
	creator = agent.SubAgentCreatorFromContext(ctx)
	if creator == nil {
		creator = agent.NewAgentCreator()
	}
	return creator
}

func loadPrompt(cfg refactorConfig, resolved resolvedPackage) (string, error) {
	b, err := fs.ReadFile(promptFS, cfg.promptPath)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s\n\nTarget package: `%s`.\n", string(b), resolved.relDir), nil
}

type refactorCASRecord struct {
	Applied bool     `json:"applied"`
	Edited  []string `json:"edited"`
}

func (cfg refactorConfig) casNamespace() gocas.Namespace {
	return gocas.Namespace(fmt.Sprintf("refactor-%s-%d", cfg.name, cfg.generation))
}

func newCASDB(moduleAbsDir string) *gocas.DB {
	return &gocas.DB{
		BaseDir: moduleAbsDir,
		DB: cas.DB{
			AbsRoot: filepath.Join(moduleAbsDir, ".codalotl", "cas"),
		},
	}
}

func (t refactorTool) newCASDB(resolved resolvedPackage) (*gocas.DB, error) {
	db := newCASDB(resolved.moduleAbsDir)
	if !pathInside(t.authorizer.SandboxDir(), db.AbsRoot) {
		return nil, fmt.Errorf("CAS root %q is outside the sandbox", db.AbsRoot)
	}
	if err := t.authorizer.IsAuthorizedForRead(false, "", ToolNameRefactor, db.AbsRoot); err != nil {
		return nil, err
	}
	if err := t.authorizer.IsAuthorizedForWrite(false, "", ToolNameRefactor, db.AbsRoot); err != nil {
		return nil, err
	}
	return db, nil
}

func codeUnitCASRecordPath(db *gocas.DB, unit *codeunit.CodeUnit, namespace gocas.Namespace) (string, error) {
	files := make([]struct {
		abs string
		rel string
	}, 0)
	seen := make(map[string]struct{})
	for _, absPath := range unit.IncludedFiles() {
		info, err := os.Stat(absPath)
		if err != nil {
			return "", err
		}
		if info.IsDir() {
			continue
		}
		if _, ok := seen[absPath]; ok {
			continue
		}
		seen[absPath] = struct{}{}
		rel, err := filepath.Rel(db.BaseDir, absPath)
		if err != nil {
			return "", err
		}
		if !relPathInside(rel) {
			return "", fmt.Errorf("code unit file %q is outside CAS base %q", absPath, db.BaseDir)
		}
		files = append(files, struct {
			abs string
			rel string
		}{abs: absPath, rel: filepath.ToSlash(rel)})
	}
	if len(files) == 0 {
		return "", errors.New("code unit has no files")
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].rel < files[j].rel
	})
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.abs)
	}
	hasher, err := cas.NewDirRelativeFileSetHasher(db.BaseDir, paths)
	if err != nil {
		return "", err
	}
	hash := hasher.Hash()
	if len(hash) < 2 {
		return "", fmt.Errorf("CAS hash %q is too short", hash)
	}
	return filepath.Join(db.AbsRoot, string(namespace), hash[:2], hash[2:]), nil
}

func resultPath(base, target string) string {
	rel, err := filepath.Rel(base, target)
	if err != nil || !relPathInside(rel) {
		return filepath.ToSlash(target)
	}
	return filepath.ToSlash(rel)
}

type codeUnitSnapshot map[string][]byte

func snapshotCodeUnitFiles(pkgAbsDir string, unit *codeunit.CodeUnit) (codeUnitSnapshot, error) {
	snap := make(codeUnitSnapshot)
	for _, absPath := range unit.IncludedFiles() {
		info, err := os.Stat(absPath)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			continue
		}
		rel, err := filepath.Rel(pkgAbsDir, absPath)
		if err != nil {
			return nil, err
		}
		b, err := os.ReadFile(absPath)
		if err != nil {
			return nil, err
		}
		snap[filepath.ToSlash(rel)] = b
	}
	return snap, nil
}

func changedFiles(before, after codeUnitSnapshot) []string {
	seen := make(map[string]struct{}, len(before)+len(after))
	for path := range before {
		seen[path] = struct{}{}
	}
	for path := range after {
		seen[path] = struct{}{}
	}

	edited := make([]string, 0, len(seen))
	for path := range seen {
		beforeBytes, beforeOK := before[path]
		afterBytes, afterOK := after[path]
		if beforeOK != afterOK || !bytes.Equal(beforeBytes, afterBytes) {
			edited = append(edited, path)
		}
	}
	sort.Strings(edited)
	return edited
}

type resolvedPackage struct {
	moduleAbsDir string
	absDir       string
	relDir       string
	importPath   string
}

func resolvePackage(authorizer authdomain.Authorizer, packageArg string) (resolvedPackage, error) {
	if authorizer == nil {
		return resolvedPackage{}, errors.New("authorizer is required")
	}
	sandboxAbsDir := authorizer.SandboxDir()
	module, err := gocode.NewModule(sandboxAbsDir)
	if err != nil {
		return resolvedPackage{}, err
	}

	var resolved resolvedPackage
	if filepath.IsAbs(packageArg) {
		rel, err := filepath.Rel(module.AbsolutePath, packageArg)
		if err != nil || !relPathInside(rel) {
			return resolvedPackage{}, fmt.Errorf("package %q is outside the current module", packageArg)
		}
		resolved, err = resolvePackageByRelativeDir(module, rel)
		if err != nil {
			return resolvedPackage{}, err
		}
	} else {
		var relErr error
		resolved, relErr = resolvePackageByRelativeDir(module, packageArg)
		if relErr != nil {
			var importErr error
			resolved, importErr = resolvePackageByImport(module, packageArg)
			if importErr != nil {
				return resolvedPackage{}, relErr
			}
		}
	}

	if !samePath(resolved.moduleAbsDir, module.AbsolutePath) {
		return resolvedPackage{}, fmt.Errorf("package %q is not in the current module", packageArg)
	}
	if !pathInside(sandboxAbsDir, resolved.absDir) {
		return resolvedPackage{}, fmt.Errorf("package %q is outside the sandbox", packageArg)
	}
	if err := authorizer.IsAuthorizedForRead(false, "", ToolNameRefactor, resolved.absDir); err != nil {
		return resolvedPackage{}, err
	}
	return resolved, nil
}

func resolvePackageByRelativeDir(module *gocode.Module, relDir string) (resolvedPackage, error) {
	moduleAbsDir, packageAbsDir, packageRelDir, fqImportPath, err := module.ResolvePackageByRelativeDir(relDir)
	if err != nil {
		return resolvedPackage{}, err
	}
	return newResolvedPackage(moduleAbsDir, packageAbsDir, packageRelDir, fqImportPath), nil
}

func resolvePackageByImport(module *gocode.Module, importPath string) (resolvedPackage, error) {
	moduleAbsDir, packageAbsDir, packageRelDir, fqImportPath, err := module.ResolvePackageByImport(importPath)
	if err != nil {
		return resolvedPackage{}, err
	}
	return newResolvedPackage(moduleAbsDir, packageAbsDir, packageRelDir, fqImportPath), nil
}

func newResolvedPackage(moduleAbsDir, packageAbsDir, packageRelDir, fqImportPath string) resolvedPackage {
	if packageRelDir == "" {
		packageRelDir = "."
	}
	return resolvedPackage{
		moduleAbsDir: moduleAbsDir,
		absDir:       packageAbsDir,
		relDir:       filepath.ToSlash(packageRelDir),
		importPath:   fqImportPath,
	}
}

func samePath(a, b string) bool {
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	return errA == nil && errB == nil && absA == absB
}

func pathInside(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	return relPathInside(rel)
}

func relPathInside(rel string) bool {
	return rel != ".." && !strings.HasPrefix(rel, "../") && !strings.HasPrefix(rel, `..\`)
}

func parseParams(input string) (Params, error) {
	dec := json.NewDecoder(strings.NewReader(input))
	dec.DisallowUnknownFields()
	var params Params
	if err := dec.Decode(&params); err != nil {
		return Params{}, err
	}
	var extra any
	if err := dec.Decode(&extra); err == nil {
		return Params{}, errors.New("multiple JSON values")
	} else if !errors.Is(err, io.EOF) {
		return Params{}, err
	}
	if params.Name == "" {
		return Params{}, errors.New("missing required field \"name\"")
	}
	if params.Package == "" {
		return Params{}, errors.New("missing required field \"package\"")
	}
	return params, nil
}

func errorToolResult(toolCall llmstream.ToolCall, err error) llmstream.ToolResult {
	res := llmstream.NewErrorToolResult(err.Error(), toolCall)
	res.Name = ToolNameRefactor
	return res
}
