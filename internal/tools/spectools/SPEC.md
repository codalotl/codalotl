# spectools

Read-mostly tools for `SPEC.md` workflows.

## check_spec_conformance

- Checks current-module Go packages for `SPEC.md` conformance.
- Skips packages without `SPEC.md`.
- Skips packages whose CAS record already says `conforms=true`.
- `only_changed=true` further restricts to packages changed against the current git comparison base.
- Runs one `limited_package_mode` subagent per package, with bounded concurrency.
- Supplies package diff and programmatic `spec diff` context to each subagent.
- Writes CAS `conforms=true` for packages that conform.
- Tool result is raw JSON keyed by module-relative package dir.
- Per-package result:
  - `conforms: true`, or
  - `conforms: false` plus `nonconformances`
- Nonconformance fields:
  - `severity`: `trivial`, `minor`, or `major`
  - `latent`: `true` when issue predates comparison base; `false` when current diff introduced it
  - `message`: human-readable explanation

## Presentation

- In progress: `Checking SPEC conformance`
- Complete: `Checked SPEC conformance`
- Completion body summarizes conforming and non-conforming packages.
- Raw `ToolResult.Result` remains machine-readable JSON.

## Public API

```go
const ToolNameCheckSpecConformance = "check_spec_conformance"

type CheckSpecConformanceToolOptions struct {
	AgentInvoker   toolsetinterface.AgentInvoker
	Model          llmmodel.ModelID
	MaxConcurrency int
}

// NewCheckSpecConformanceTool creates a tool that checks SPEC.md conformance for packages in the current module and records conforming packages in CAS.
func NewCheckSpecConformanceTool(authorizer authdomain.Authorizer, options ...CheckSpecConformanceToolOptions) llmstream.Tool
```
