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
	- successfully applied refactor
	- refactor not needed
	- error

## Refactors

### docs-add

- Delegates to `codalotl docs add --public-only <package>`.
- No CAS.
- Uses `codalotl_cli` command execution and visible stdout streaming.

### dry

Prompt-style refactor.

- Looks for opportunities to share helpers inside a package.
- Can create missing helper functions, use them, and combine similar helper functions.
- Uses `limited_package_mode`.
- Uses code-unit CAS:
	- already-certified current code unit is an error.
	- after a successful run, certify post-run code unit.
	- unchanged hash returns `not_needed`.

## Prompt-style refactors

Prompt-style refactors are defined by name, `data/` prompt, agent name, and CAS policy (`none` or `code-unit`).

## Presentation

- Summary:
	- In progress: `Refactoring docs-add in internal/foo`
	- Complete: `Refactored docs-add in internal/foo`
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
	ResultStatusApplied   ResultStatus = "applied"
	ResultStatusNotNeeded ResultStatus = "not_needed"
)

// Result is the machine-readable refactor tool result.
type Result struct {
	Name    string       `json:"name"`
	Package string       `json:"package"`
	Status  ResultStatus `json:"status"`
	Message string       `json:"message,omitempty"`
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
