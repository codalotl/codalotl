# refactor

`refactor` provides one agent-facing tool for package-local canned refactors.

Refactors may be prompt-style agent runs or code-driven operations. The tool keeps the LLM-facing surface small while making new package-local refactors easy to add.

## Behavior

- Tool name: `refactor`.
- Params:
	- `name string`: refactor name.
	- `package string`: Go package directory, current-module import path, or current-module relative package path.
- Package resolution must reject packages outside the sandbox, including stdlib and module dependencies.
- Tool description lists available refactor names and brief descriptions.
- Unknown names are usage errors.
- Tool results distinguish:
	- "successfully applied refactor" - the refactor was done and code was edited.
	- "no refactoring opportunities found" - the refactoring logic was applied, but no edits were made. Code was already in a nicely factored state.
	- "refactor already applied" - for CAS-backed refactors only, a CAS record already indicates the refactor was applied, so the refactor is skipped.
	- error
- Tool result always includes edited file list. It includes saved CAS record path only for refactor-owned CAS writes.

### CAS

- Each refactor is flagged with one of: `cas-ignore` or `cas-code-unit`
- `cas-ignore` means the refactor does not use refactor-owned CAS records. It may delegate to commands that write their own CAS records or consume external workflow CAS records.
- `cas-ignore` refactors never return "refactor already applied".
- `cas-code-unit` means the refactor checks/stores a CAS cache entry to record that the refactor was done:
	- Uses a `gocas.NamespaceSpec` with `HashModeCodeUnit`.
	- Before running, CAS hit returns "refactor already applied".
	- After the refactor, we store a CAS entry.
		- Namespace spec name is prefixed with `refactor`, like `refactor-dry`; version is the refactor generation, yielding filesystem namespace like `refactor-dry-1`.
		- Metadata is a JSON object containing `"applied": true` and a list of files edited (package-relative). Ex: `{"applied": true, "edited": ["foo.go"]}`. If no files were edited, `edited` can be `null` or `[]`.
	- On refactor error, no CAS is written.
	- If writing to CAS results in an error, the overall tool returns an error. Do not attempt file cleanup. Files can remain edited. That's fine.

### Detecting Edits

Before the refactor is run, remember hash or bytes of original code unit files. Afterwards, see if any changed by re-reading. Detect deleted or added files. Moved files are just adds and removes.

## Refactors

### docs-add

- Delegates to `codalotl docs add --important <package>`. Uses `codalotl_cli` command execution and visible stdout streaming.
- CAS: `cas-ignore`

### docs-fix

- Delegates to `codalotl docs fix <package>` via `codalotl_cli`, with visible stdout streaming.
- CAS: refactor-level `cas-ignore`; delegated CLI command writes the docs-fix CAS record.
- Result reports edited files, not delegated CLI CAS records.

### docs-improve-from-clarify

Prompt-style documentation refactor.

- Uses in-play `clarify_public_api` CAS Q/A records for target package.
- Invokes `package_mode_default_context` with prompt and Q/As.
- May improve any package docs that resolve public API confusion, not just docs on questioned identifier.
- On success, deletes consumed clarify records, including no-op runs. On failure, preserves them.
- CAS: `cas-ignore`; clarify CAS records are external workflow state.

### dry

Prompt-style refactor.

- Prompt: Looks for opportunities to share helpers inside a package. Can create missing helper functions, use them, and combine similar helper functions.
- Agent: `limited_package_mode`.
- CAS: `cas-code-unit`.

### test-cleanup

Prompt-style refactor.

- Prompt: Applies `$go-testing` hygiene to existing package tests. May remove or coalesce redundant tests, add maintainable helpers, and convert tests to table-driven form when useful.
- Not intended to add missing test coverage or radically refactor tests.
- Agent: `limited_package_mode`.
- CAS: `cas-code-unit`.

### test-ensure-coverage

Prompt-style refactor.

- Prompt: Uses `$go-testing` and Go coverage tooling to add worthwhile tests for public APIs and important edge cases.
- Supplements `test-cleanup`; does not primarily refactor tests.
- Agent: `limited_package_mode`.
- CAS: `cas-code-unit`.

## Prompt-style refactors

Prompt-style refactors are defined by name, a Markdown prompt file in `data/`, agent name, and CAS policy.

## Presentation

- Summary:
	- In progress: `Refactoring docs-add in internal/foo`
	- Complete: `Refactored docs-add in internal/foo`
- Summary uses semantic roles: action verb as action; refactor/package details as normal text.
- Complete presentation includes a status detail line, like `Refactor already applied`.
- Behavior: Append
- Prompt-style refactors show normal descendant subagent events and do not hide descendant final messages.
- `docs-add` and `docs-fix` visible stdout are owned by delegated `codalotl_cli` behavior.

## Public API

```go {api}
const ToolNameRefactor = "refactor"
```

```go {api}
// CASNamespaceSpecs returns refactor-owned CAS namespace specs for code-unit CAS-backed refactors.
//
// cas-ignore refactors are omitted.
func CASNamespaceSpecs() []gocas.NamespaceSpec
```

```go {api}
// Params are the refactor tool parameters.
type Params struct {
	Name    string `json:"name"`
	Package string `json:"package"`
}
```

```go {api}
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
	SavedCASRecord *string      `json:"saved-cas-record,omitempty"`
}
```

```go {api}
// Options configures the refactor tool.
type Options struct {
	AgentInvoker   toolsetinterface.AgentInvoker
	Model          llmmodel.ModelID
	LintSteps      []lints.Step
	NewCommandTree toolcli.CommandTreeFunc
}

// NewRefactorTool creates the refactor tool.
func NewRefactorTool(authorizer authdomain.Authorizer, options Options) llmstream.Tool
```
