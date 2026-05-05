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
- Tool result always includes edited file list and saved CAS record path/null.

### CAS

- Each refactor is flagged with one of: `cas-ignore` or `cas-code-unit`
- `cas-ignore` means the refactor does not use the CAS system. For instance, `codalotl docs add` doesn't need a separate CAS record to keep track of whether docs are added - it's better to look at the code itself to see if anything lacks documentation.
- `cas-ignore` refactors never return "refactor already applied".
- `cas-code-unit` means the refactor checks/stores a CAS cache entry to record that the refactor was done:
	- Code unit means the CAS hash is based on code units.
	- Before running, CAS hit returns "refactor already applied".
	- After the refactor, we store a CAS entry.
		- Namespace is prefixed with `refactor` and suffixed with a generation, like `refactor-dry-1`.
		- Metadata is a JSON object containing `"applied": true` and a list of files edited (package-relative). Ex: `{"applied": true, "edited": ["foo.go"]}`. If no files were edited, `edited` can be `null` or `[]`.
	- On refactor error, no CAS is written.
	- If writing to CAS results in an error, the overall tool returns an error. Do not attempt file cleanup. Files can remain edited. That's fine.

### Detecting Edits

Before the refactor is run, remember hash or bytes of original code unit files. Afterwards, see if any changed by re-reading. Detect deleted or added files. Moved files are just adds and removes.

## Refactors

### docs-add

- Delegates to `codalotl docs add --public-only <package>`. Uses `codalotl_cli` command execution and visible stdout streaming.
- CAS: `cas-ignore`

### dry

Prompt-style refactor.

- Prompt: Looks for opportunities to share helpers inside a package. Can create missing helper functions, use them, and combine similar helper functions.
- Agent: `limited_package_mode`.
- CAS: `cas-code-unit`.

## Prompt-style refactors

Prompt-style refactors are defined by name, a Markdown prompt file in `data/`, agent name, and CAS policy.

## Presentation

- Summary:
	- In progress: `Refactoring docs-add in internal/foo`
	- Complete: `Refactored docs-add in internal/foo`
- Summary uses semantic roles: action verb as action; refactor/package details styled.
- Complete presentation includes a status detail line, like `Refactor already applied`.
- Behavior: Append
- Prompt-style refactors show normal descendant subagent events and do not hide descendant final messages.
- `docs-add` visible stdout is owned by delegated `codalotl_cli` behavior.

## Public API

```go {api}
const ToolNameRefactor = "refactor"
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
	SavedCASRecord *string      `json:"saved-cas-record"`
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
