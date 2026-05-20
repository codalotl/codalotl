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
	"github.com/codalotl/codalotl/internal/gocas/casclarify"
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
	Name    string `json:"name"`    // Name is the registered refactor name to run.
	Package string `json:"package"` // Package is the target package as an absolute directory, current-module-relative directory, or current-module import path.
}

// ResultStatus describes the outcome of a refactor run.
type ResultStatus string

// ResultStatus values describe the outcome of a refactor run.
const (
	// ResultStatusApplied means the refactor was applied.
	ResultStatusApplied ResultStatus = "applied"

	// ResultStatusNoOpportunity means the refactor found no applicable change.
	ResultStatusNoOpportunity ResultStatus = "no_opportunity"

	// ResultStatusAlreadyApplied means a CAS-backed refactor had already been applied to the code unit.
	ResultStatusAlreadyApplied ResultStatus = "already_applied"
)

const (
	resultMessageApplied        = "successfully applied refactor"
	resultMessageNoOpportunity  = "no refactoring opportunities found"
	resultMessageAlreadyApplied = "refactor already applied"
)

// Result is the machine-readable refactor tool result.
type Result struct {
	Name           string       `json:"name"`                       // Name is the refactor name that ran.
	Package        string       `json:"package"`                    // Package is the resolved package directory relative to the module root.
	Status         ResultStatus `json:"status"`                     // Status is the machine-readable outcome of the refactor run.
	Message        string       `json:"message,omitempty"`          // Message is a human-readable description of Status.
	EditedFiles    []string     `json:"edited-files"`               // EditedFiles lists package-relative, slash-separated files whose contents or existence changed.
	SavedCASRecord *string      `json:"saved-cas-record,omitempty"` // SavedCASRecord is the path to the refactor-owned CAS record written for the run.
}

// Options configures the refactor tool.
type Options struct {
	AgentInvoker   toolsetinterface.AgentInvoker // AgentInvoker invokes subagents for prompt-style refactors.
	Model          llmmodel.ModelID              // Model is the model used by prompt-style refactor agents.
	LintSteps      []lints.Step                  // LintSteps configures linting for prompt-style refactor agents.
	NewCommandTree toolcli.CommandTreeFunc       // NewCommandTree creates the whitelisted codalotl command tree used by docs refactors.
}

//go:embed data/*.md
var promptFS embed.FS

var findInPlayClarifyRecords = casclarify.FindInPlay

// casPolicy selects how a refactor uses content-addressable storage.
type casPolicy string

const (
	casPolicyIgnore   casPolicy = "cas-ignore"
	casPolicyCodeUnit casPolicy = "cas-code-unit"
)

// refactorKind selects the implementation strategy for a refactor.
type refactorKind string

const (
	refactorKindDocsAdd                refactorKind = "docs-add"
	refactorKindDocsFix                refactorKind = "docs-fix"
	refactorKindDocsImproveFromClarify refactorKind = "docs-improve-from-clarify"
	refactorKindPrompt                 refactorKind = "prompt"
)

// refactorConfig describes one registered canned refactor.
type refactorConfig struct {
	name        string       // name is the refactor name accepted in tool parameters.
	description string       // description is the human-readable summary shown in tool metadata.
	kind        refactorKind // kind selects the implementation strategy for the refactor.
	casPolicy   casPolicy    // casPolicy selects how the refactor uses CAS.
	promptPath  string       // promptPath is the embedded prompt path for prompt-style refactors.
	agentName   string       // agentName is the subagent name used for prompt-style refactors.
	generation  int          // generation versions CAS records for this refactor configuration.
}

var refactorRegistry = []refactorConfig{
	{
		name:        "docs-add",
		description: "Add missing important Go documentation with codalotl docs add.",
		kind:        refactorKindDocsAdd,
		casPolicy:   casPolicyIgnore,
	},
	{
		name:        "docs-fix",
		description: "Fix materially false Go documentation with codalotl docs fix.",
		kind:        refactorKindDocsFix,
		casPolicy:   casPolicyIgnore,
	},
	{
		name:        "docs-improve-from-clarify",
		description: "Improve public Go documentation from clarify_public_api Q/A records.",
		kind:        refactorKindDocsImproveFromClarify,
		casPolicy:   casPolicyIgnore,
		promptPath:  "data/docs-improve-from-clarify.md",
		agentName:   "package_mode_default_context",
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
	{
		name:        "test-cleanup",
		description: "Clean up existing Go tests without adding missing coverage.",
		kind:        refactorKindPrompt,
		casPolicy:   casPolicyCodeUnit,
		promptPath:  "data/test-cleanup.md",
		agentName:   "limited_package_mode",
		generation:  1,
	},
	{
		name:        "test-ensure-coverage",
		description: "Add worthwhile Go test coverage for public APIs and important edge cases.",
		kind:        refactorKindPrompt,
		casPolicy:   casPolicyCodeUnit,
		promptPath:  "data/test-ensure-coverage.md",
		agentName:   "limited_package_mode",
		generation:  1,
	},
}

// CASNamespaceSpecs returns refactor-owned CAS namespace specs for code-unit CAS-backed refactors.
//
// cas-ignore refactors are omitted.
func CASNamespaceSpecs() []gocas.NamespaceSpec {
	specs := make([]gocas.NamespaceSpec, 0)
	for _, cfg := range refactorRegistry {
		if cfg.casPolicy != casPolicyCodeUnit {
			continue
		}
		specs = append(specs, cfg.casNamespaceSpec())
	}
	return specs
}

// refactorTool applies registered package-local refactors.
type refactorTool struct {
	authorizer authdomain.Authorizer     // authorizer controls package and CAS filesystem access.
	options    Options                   // options supplies runtime dependencies for refactor execution.
	registry   map[string]refactorConfig // registry maps refactor names to their configurations.
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

// Info returns the LLM-facing metadata and parameter schema for the refactor tool.
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

// Name returns the refactor tool name.
func (t refactorTool) Name() string {
	return ToolNameRefactor
}

// Presenter returns the presenter used to render refactor calls and results.
func (t refactorTool) Presenter() llmstream.Presenter {
	return refactorPresenter{}
}

// Run executes the requested refactor and returns its tool result.
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
	case refactorKindDocsFix:
		result, err = t.runDocsFix(ctx, resolved, cfg)
	case refactorKindDocsImproveFromClarify:
		result, err = t.runDocsImproveFromClarify(ctx, resolved, cfg)
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

// description returns the LLM-facing description of available refactors and package constraints.
func (t refactorTool) description() string {
	var b strings.Builder
	b.WriteString("Apply a package-local canned refactor. Available refactors:\n")
	for _, cfg := range refactorRegistry {
		fmt.Fprintf(&b, "- %s: %s\n", cfg.name, cfg.description)
	}
	b.WriteString("Package must be in the current module and inside the sandbox.")
	return b.String()
}

func newRefactorResult(cfg refactorConfig, resolved resolvedPackage, status ResultStatus, edited []string, savedCASRecord *string) Result {
	return Result{
		Name:           cfg.name,
		Package:        resolved.relDir,
		Status:         status,
		Message:        refactorStatusMessage(status),
		EditedFiles:    edited,
		SavedCASRecord: savedCASRecord,
	}
}

func refactorStatusMessage(status ResultStatus) string {
	switch status {
	case ResultStatusApplied:
		return resultMessageApplied
	case ResultStatusNoOpportunity:
		return resultMessageNoOpportunity
	case ResultStatusAlreadyApplied:
		return resultMessageAlreadyApplied
	default:
		return ""
	}
}

func refactorAppliedStatus(noOpportunity bool) ResultStatus {
	if noOpportunity {
		return ResultStatusNoOpportunity
	}
	return ResultStatusApplied
}

// runDocsAdd runs the docs-add refactor for resolved.
func (t refactorTool) runDocsAdd(ctx context.Context, resolved resolvedPackage, cfg refactorConfig) (Result, error) {
	if cfg.casPolicy != casPolicyIgnore {
		return Result{}, fmt.Errorf("docs-add refactor requires CAS policy %q", casPolicyIgnore)
	}
	if t.options.NewCommandTree == nil {
		return Result{}, errors.New("docs-add refactor requires NewCommandTree")
	}

	tracker, err := newDefaultGoCodeUnitChangeTracker(resolved.absDir)
	if err != nil {
		return Result{}, err
	}

	cliTool := toolcli.NewCodalotlCLITool(t.options.NewCommandTree)
	cliParams := toolcli.Params{
		Subcommand: "docs",
		Argv:       []string{"add", "--important", resolved.absDir},
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

	_, edited, err := tracker.changedFiles()
	if err != nil {
		return Result{}, err
	}

	status := refactorAppliedStatus(docsAddNoOpportunity(parsed.Stdout))
	return newRefactorResult(cfg, resolved, status, edited, nil), nil
}

func docsAddNoOpportunity(stdout string) bool {
	return strings.Contains(stdout, "Nothing left to document!") &&
		!strings.Contains(stdout, "Applied ")
}

// runDocsFix runs the docs-fix refactor for resolved.
func (t refactorTool) runDocsFix(ctx context.Context, resolved resolvedPackage, cfg refactorConfig) (Result, error) {
	if cfg.casPolicy != casPolicyIgnore {
		return Result{}, fmt.Errorf("docs-fix refactor requires CAS policy %q", casPolicyIgnore)
	}
	if t.options.NewCommandTree == nil {
		return Result{}, errors.New("docs-fix refactor requires NewCommandTree")
	}

	tracker, err := newDefaultGoCodeUnitChangeTracker(resolved.absDir)
	if err != nil {
		return Result{}, err
	}

	cliTool := toolcli.NewCodalotlCLITool(t.options.NewCommandTree)
	cliParams := toolcli.Params{
		Subcommand: "docs",
		Argv:       []string{"fix", resolved.absDir},
	}
	input, err := json.Marshal(cliParams)
	if err != nil {
		return Result{}, err
	}

	cliResult := cliTool.Run(ctx, llmstream.ToolCall{
		CallID: "refactor-docs-fix",
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
		msg := "codalotl docs fix failed"
		if parsed.Stderr != "" {
			msg = parsed.Stderr
		} else if parsed.Stdout != "" {
			msg = parsed.Stdout
		}
		return Result{}, errors.New(msg)
	}

	_, edited, err := tracker.changedFiles()
	if err != nil {
		return Result{}, err
	}

	status := refactorAppliedStatus(len(edited) == 0)
	return newRefactorResult(cfg, resolved, status, edited, nil), nil
}

// runDocsImproveFromClarify runs the clarify-public-api documentation improvement refactor.
func (t refactorTool) runDocsImproveFromClarify(ctx context.Context, resolved resolvedPackage, cfg refactorConfig) (Result, error) {
	if cfg.casPolicy != casPolicyIgnore {
		return Result{}, fmt.Errorf("docs-improve-from-clarify refactor requires CAS policy %q", casPolicyIgnore)
	}

	db, err := t.newReadCASDB(resolved)
	if err != nil {
		return Result{}, err
	}
	mod, err := gocode.NewModule(resolved.moduleAbsDir)
	if err != nil {
		return Result{}, err
	}
	records, err := findInPlayClarifyRecords(db, mod)
	if err != nil {
		return Result{}, err
	}
	entries, consumedRecords := clarifyEntriesForPackage(records, resolved.importPath)
	if len(entries) == 0 {
		return newRefactorResult(cfg, resolved, ResultStatusNoOpportunity, []string{}, nil), nil
	}
	if t.options.AgentInvoker == nil {
		return Result{}, errors.New("docs-improve-from-clarify refactor requires AgentInvoker")
	}

	tracker, err := newDefaultGoCodeUnitChangeTracker(resolved.absDir)
	if err != nil {
		return Result{}, err
	}
	prompt, err := docsImproveFromClarifyPrompt(cfg, resolved, entries)
	if err != nil {
		return Result{}, err
	}
	if err := t.invokePromptAgent(ctx, resolved, cfg, prompt, tracker.beforeUnit); err != nil {
		return Result{}, err
	}

	_, edited, err := tracker.changedFiles()
	if err != nil {
		return Result{}, err
	}
	if err := t.deleteClarifyRecords(db, consumedRecords); err != nil {
		return Result{}, err
	}

	status := refactorAppliedStatus(len(edited) == 0)
	return newRefactorResult(cfg, resolved, status, edited, nil), nil
}

func clarifyEntriesForPackage(records []casclarify.InPlayRecord, targetPackage string) ([]casclarify.Entry, []casclarify.InPlayRecord) {
	var entries []casclarify.Entry
	var consumedRecords []casclarify.InPlayRecord
	for _, record := range records {
		recordEntries := clarifyRecordEntriesForPackage(record, targetPackage)
		if len(recordEntries) == 0 {
			continue
		}
		entries = append(entries, recordEntries...)
		consumedRecords = append(consumedRecords, record)
	}
	return entries, consumedRecords
}

func clarifyRecordEntriesForPackage(record casclarify.InPlayRecord, targetPackage string) []casclarify.Entry {
	var entries []casclarify.Entry
	for _, entry := range record.Metadata.Entries {
		if clarifyEntryTargetPackage(record, entry) == targetPackage {
			entries = append(entries, entry)
		}
	}
	return entries
}

func clarifyEntryTargetPackage(record casclarify.InPlayRecord, entry casclarify.Entry) string {
	target := strings.TrimSpace(entry.TargetPackage)
	if target == "" {
		target = strings.TrimSpace(record.TargetPackage)
	}
	return target
}

func docsImproveFromClarifyPrompt(cfg refactorConfig, resolved resolvedPackage, entries []casclarify.Entry) (string, error) {
	b, err := fs.ReadFile(promptFS, cfg.promptPath)
	if err != nil {
		return "", err
	}

	records := docsImproveFromClarifyRecordsPrompt(entries)
	prompt := strings.NewReplacer(
		"{{PACKAGE_REL_DIR}}", resolved.relDir,
		"{{PACKAGE_IMPORT_PATH}}", resolved.importPath,
		"{{CLARIFY_QA_RECORDS}}", records,
	).Replace(string(b))
	return prompt, nil
}

func docsImproveFromClarifyRecordsPrompt(entries []casclarify.Entry) string {
	var b strings.Builder
	for i, entry := range entries {
		fmt.Fprintf(&b, "### %d. %s\n\n", i+1, clarifyPromptIdentifier(entry.Identifier))
		if entry.OriginPackage != "" {
			fmt.Fprintf(&b, "- Origin package: `%s`\n", entry.OriginPackage)
		}
		if target := entry.TargetPackage; target != "" {
			fmt.Fprintf(&b, "- Target package: `%s`\n", target)
		}
		if entry.OriginPackage != "" || entry.TargetPackage != "" {
			b.WriteString("\n")
		}
		writeClarifyPromptBlock(&b, "Question", entry.Question)
		writeClarifyPromptBlock(&b, "Answer", entry.Answer)
	}
	return b.String()
}

func clarifyPromptIdentifier(identifier string) string {
	if identifier == "" {
		return "(unspecified identifier)"
	}
	return fmt.Sprintf("`%s`", identifier)
}

func writeClarifyPromptBlock(b *strings.Builder, label string, text string) {
	fmt.Fprintf(b, "%s:\n", label)
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) == 0 {
		lines = []string{""}
	}
	for _, line := range lines {
		fmt.Fprintf(b, "> %s\n", line)
	}
	b.WriteString("\n")
}

func (t refactorTool) deleteClarifyRecords(db *gocas.DB, records []casclarify.InPlayRecord) error {
	if len(records) == 0 {
		return nil
	}
	if err := t.authorizer.IsAuthorizedForWrite(false, "", ToolNameRefactor, db.AbsRoot); err != nil {
		return err
	}

	seen := make(map[string]struct{}, len(records))
	for _, record := range records {
		if _, ok := seen[record.Path]; ok {
			continue
		}
		seen[record.Path] = struct{}{}
		if !pathInside(db.AbsRoot, record.Path) {
			return fmt.Errorf("clarify record %q is outside CAS root %q", record.Path, db.AbsRoot)
		}
		if err := record.Delete(); err != nil {
			return err
		}
	}
	return nil
}

// runPromptRefactor runs a CAS-backed prompt-style refactor for resolved.
func (t refactorTool) runPromptRefactor(ctx context.Context, resolved resolvedPackage, cfg refactorConfig) (Result, error) {
	if t.options.AgentInvoker == nil {
		return Result{}, errors.New("prompt-style refactor requires AgentInvoker")
	}
	if cfg.casPolicy != casPolicyCodeUnit {
		return Result{}, fmt.Errorf("unsupported CAS policy %q", cfg.casPolicy)
	}

	tracker, err := newDefaultGoCodeUnitChangeTracker(resolved.absDir)
	if err != nil {
		return Result{}, err
	}
	db, err := t.newCASDB(resolved)
	if err != nil {
		return Result{}, err
	}
	pkg, err := loadResolvedPackage(resolved)
	if err != nil {
		return Result{}, err
	}
	spec := cfg.casNamespaceSpec()
	var casRecord refactorCASRecord
	ok, _, err := db.Retrieve(pkg, spec, &casRecord)
	if err != nil {
		return Result{}, err
	}
	if ok {
		// Only CAS-backed refactors report already_applied.
		return newRefactorResult(cfg, resolved, ResultStatusAlreadyApplied, []string{}, nil), nil
	}

	prompt, err := loadPrompt(cfg, resolved)
	if err != nil {
		return Result{}, err
	}
	if err := t.invokePromptAgent(ctx, resolved, cfg, prompt, tracker.beforeUnit); err != nil {
		return Result{}, err
	}

	afterUnit, edited, err := tracker.changedFiles()
	if err != nil {
		return Result{}, err
	}
	afterPkg, err := loadResolvedPackage(resolved)
	if err != nil {
		return Result{}, err
	}
	casRecordAbsPath, err := codeUnitCASRecordPath(db, afterUnit, spec)
	if err != nil {
		return Result{}, err
	}
	if err := db.Store(afterPkg, spec, refactorCASRecord{Applied: true, Edited: edited}); err != nil {
		return Result{}, err
	}
	savedCASRecord := resultPath(resolved.moduleAbsDir, casRecordAbsPath)

	status := refactorAppliedStatus(len(edited) == 0)
	return newRefactorResult(cfg, resolved, status, edited, &savedCASRecord), nil
}

// invokePromptAgent runs cfg's prompt agent for resolved and waits for successful completion.
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
			return agentEventError(event, errors.New("prompt refactor agent failed"))
		case agent.EventTypeCanceled:
			return agentEventError(event, context.Canceled)
		case agent.EventTypeDoneSuccess:
			terminal = true
		}
	}
	if !terminal {
		return errors.New("prompt refactor agent ended without success")
	}
	return nil
}

func agentEventError(event agent.Event, fallback error) error {
	if event.Error != nil {
		return event.Error
	}
	return fallback
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

// refactorCASRecord records prompt-style refactor metadata stored in CAS.
type refactorCASRecord struct {
	Applied bool     `json:"applied"` // Applied reports whether the refactor was applied for the code unit.
	Edited  []string `json:"edited"`  // Edited lists package-relative files changed when the record was created.
}

// casNamespaceSpec returns the CAS namespace spec for cfg.
func (cfg refactorConfig) casNamespaceSpec() gocas.NamespaceSpec {
	return gocas.NamespaceSpec{
		Name:     "refactor-" + cfg.name,
		Version:  cfg.generation,
		HashMode: gocas.HashModeCodeUnit,
	}
}

// newCASDB returns an authorized CAS database for resolved's module.
func (t refactorTool) newCASDB(resolved resolvedPackage) (*gocas.DB, error) {
	db, err := t.newReadCASDB(resolved)
	if err != nil {
		return nil, err
	}
	if err := t.authorizer.IsAuthorizedForWrite(false, "", ToolNameRefactor, db.AbsRoot); err != nil {
		return nil, err
	}
	return db, nil
}

// newReadCASDB returns a read-authorized CAS database for resolved's module.
func (t refactorTool) newReadCASDB(resolved resolvedPackage) (*gocas.DB, error) {
	db, err := gocas.NewDBForBaseDir(resolved.moduleAbsDir)
	if err != nil {
		return nil, err
	}
	if err := t.authorizer.IsAuthorizedForRead(false, "", ToolNameRefactor, db.AbsRoot); err != nil {
		return nil, err
	}
	return db, nil
}

func loadResolvedPackage(resolved resolvedPackage) (*gocode.Package, error) {
	mod, err := gocode.NewModule(resolved.moduleAbsDir)
	if err != nil {
		return nil, err
	}
	return mod.LoadPackageByRelativeDir(resolved.relDir)
}

// codeUnitCASRecordPath returns the absolute CAS record path for unit in spec.
func codeUnitCASRecordPath(db *gocas.DB, unit *codeunit.CodeUnit, spec gocas.NamespaceSpec) (string, error) {
	if spec.HashMode != gocas.HashModeCodeUnit {
		return "", fmt.Errorf("CAS record path requires hash mode %q, got %q", gocas.HashModeCodeUnit, spec.HashMode)
	}
	includedFiles, err := nonDirCodeUnitFiles(unit)
	if err != nil {
		return "", err
	}
	files := make([]struct {
		abs string
		rel string
	}, 0)
	seen := make(map[string]struct{})
	for _, absPath := range includedFiles {
		if _, ok := seen[absPath]; ok {
			continue
		}
		seen[absPath] = struct{}{}
		rel, inside, err := relPathWithin(db.BaseDir, absPath)
		if err != nil {
			return "", err
		}
		if !inside {
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
	return filepath.Join(db.AbsRoot, string(spec.Namespace()), hash[:2], hash[2:]), nil
}

func resultPath(base, target string) string {
	rel, inside, err := relPathWithin(base, target)
	if err != nil || !inside {
		return filepath.ToSlash(target)
	}
	return filepath.ToSlash(rel)
}

// codeUnitSnapshot maps package-relative file paths to their captured contents.
type codeUnitSnapshot map[string][]byte

// defaultGoCodeUnitChangeTracker tracks changes to a package's default Go code unit.
type defaultGoCodeUnitChangeTracker struct {
	pkgAbsDir   string             // pkgAbsDir is the absolute path to the tracked package directory.
	beforeUnit  *codeunit.CodeUnit // beforeUnit is the default Go code unit captured before changes.
	beforeFiles codeUnitSnapshot   // beforeFiles is the file snapshot captured before changes.
}

func newDefaultGoCodeUnitChangeTracker(pkgAbsDir string) (*defaultGoCodeUnitChangeTracker, error) {
	beforeUnit, beforeFiles, err := snapshotDefaultGoCodeUnit(pkgAbsDir)
	if err != nil {
		return nil, err
	}
	return &defaultGoCodeUnitChangeTracker{
		pkgAbsDir:   pkgAbsDir,
		beforeUnit:  beforeUnit,
		beforeFiles: beforeFiles,
	}, nil
}

// changedFiles returns the current code unit and files changed since the tracker was created.
func (t defaultGoCodeUnitChangeTracker) changedFiles() (*codeunit.CodeUnit, []string, error) {
	afterUnit, afterFiles, err := snapshotDefaultGoCodeUnit(t.pkgAbsDir)
	if err != nil {
		return nil, nil, err
	}
	return afterUnit, changedFiles(t.beforeFiles, afterFiles), nil
}

func snapshotDefaultGoCodeUnit(pkgAbsDir string) (*codeunit.CodeUnit, codeUnitSnapshot, error) {
	unit, err := codeunit.DefaultGoCodeUnit(pkgAbsDir)
	if err != nil {
		return nil, nil, err
	}
	files, err := snapshotCodeUnitFiles(pkgAbsDir, unit)
	if err != nil {
		return nil, nil, err
	}
	return unit, files, nil
}

func snapshotCodeUnitFiles(pkgAbsDir string, unit *codeunit.CodeUnit) (codeUnitSnapshot, error) {
	includedFiles, err := nonDirCodeUnitFiles(unit)
	if err != nil {
		return nil, err
	}
	snap := make(codeUnitSnapshot)
	for _, absPath := range includedFiles {
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

func nonDirCodeUnitFiles(unit *codeunit.CodeUnit) ([]string, error) {
	files := make([]string, 0)
	for _, absPath := range unit.IncludedFiles() {
		info, err := os.Stat(absPath)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			continue
		}
		files = append(files, absPath)
	}
	return files, nil
}

// changedFiles returns sorted paths that were added, removed, or modified between snapshots.
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

// resolvedPackage describes a Go package resolved within the current module and sandbox.
type resolvedPackage struct {
	moduleAbsDir string // moduleAbsDir is the absolute path to the module root.
	absDir       string // absDir is the absolute path to the package directory.
	relDir       string // relDir is the slash-separated package directory relative to moduleAbsDir, or "." for the module root.
	importPath   string // importPath is the package's fully qualified Go import path.
}

// resolvePackage resolves and authorizes packageArg within the current module and sandbox.
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
		rel, inside, err := relPathWithin(module.AbsolutePath, packageArg)
		if err != nil || !inside {
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

// packageResolver resolves a package argument to module, package, and import-path metadata.
type packageResolver func(packageArg string) (moduleAbsDir, packageAbsDir, packageRelDir, fqImportPath string, err error)

func resolvePackageByRelativeDir(module *gocode.Module, relDir string) (resolvedPackage, error) {
	return resolvePackageWith(module.ResolvePackageByRelativeDir, relDir)
}

func resolvePackageByImport(module *gocode.Module, importPath string) (resolvedPackage, error) {
	return resolvePackageWith(module.ResolvePackageByImport, importPath)
}

func resolvePackageWith(resolve packageResolver, packageArg string) (resolvedPackage, error) {
	moduleAbsDir, packageAbsDir, packageRelDir, fqImportPath, err := resolve(packageArg)
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
	_, inside, err := relPathWithin(base, target)
	return err == nil && inside
}

func relPathWithin(base, target string) (string, bool, error) {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return "", false, err
	}
	return rel, relPathInside(rel), nil
}

func relPathInside(rel string) bool {
	return rel != ".." && !strings.HasPrefix(rel, "../") && !strings.HasPrefix(rel, `..\`)
}

// parseParams decodes and validates a refactor tool JSON input object.
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
