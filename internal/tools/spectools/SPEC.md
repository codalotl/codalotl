# spectools

Tools for `SPEC.md` workflows.

## check_spec_conformance

This tool checks Go packages in this module for `SPEC.md` conformance. It accepts one parameter: `only_changed`. It returns JSON that indicates which checked packages conform and which do not. It has the side-effect of automatically doing CAS writes for any conforming package.

Details:
- Skips packages without `SPEC.md`.
- Skips packages whose CAS record already says `conforms=true` when package scope has no diff against comparison base.
- `only_changed=true` further restricts to packages whose on-disk state changed against the current git comparison base. See `### Diffing`
- `only_changed=false` checks all current-module packages that have `SPEC.md`, except packages already marked `conforms=true` in CAS when package scope has no diff.
- If no packages are eligible, tool returns `{}`.
- Runs one `limited_package_mode` subagent per package, with bounded concurrency.
- Labels each package-check subagent with the module-relative package dir.
- Supplies package diff and programmatic `spec diff` context to each subagent.
- Subagent instructions remain read-only in intent and use the `$spec-md` check-conformance workflow.
- Package scope is the default Go code unit rooted at the package dir.
    - This same scope is used for the subagent authorizer, changed-path attribution, and conforming-package CAS reuse / invalidation.
- Tool result is raw JSON keyed by module-relative package dir. Only checked packages appear.
- If a checked package has no diff against the comparison base, any reported nonconformance is `latent=true`.
- CAS writes:
    - Happen for a package as soon as that package's result comes in.
    - write `conforms=true` only for conforming packages
    - do not store nonconforming results
- Fail overall tool call only for pre-launch or global failures.
- Once package checking starts, record package-scoped failures in that package's object instead of failing overall tool call.
- Package-scoped failures in `error` include subagent errors and per-package preparation or parsing failures.
- Post-verdict package side-effect failures are recorded in `postcheck_error`, not `error`.
- Once we start launching subagents, keep surface area of overall tool call errors to a minimum. It would be surprising if the overall tool call failed, but some CAS entries were still written.

### Result

Example:
```json
{
    "internal/foo": {
        "conforms": true
    },
    "internal/bar": {
        "conforms": false,
        "nonconformances": [
            {"severity": "trivial", "latent": true, "message": "The spec demands X, but instead we have Y"},
            {"severity": "major", "latent": false, "message": "explanation"}
        ]
    },
    "internal/baz": {
        "error": "timed out"
    },
    "internal/qux": {
        "conforms": true,
        "postcheck_error": "store CAS conformance: permission denied"
    },
}
```

Notes:
- `conforms=true` omits `nonconformances`; explicit `null` is equivalent to omission
- `conforms=false` includes one or more `nonconformances`
- `postcheck_error` may coexist with a valid verdict
- Any other result-shape combination is invalid and is treated as a package-scoped error
- `severity`: `trivial`, `minor`, or `major`
- `latent`: `true` when issue predates comparison base; `false` when current diff introduced it
- `message`: human-readable explanation
- `error`: only if package checking for that package fails before producing a valid verdict. Value is a string error message.
- `postcheck_error`: only if per-package work after a valid verdict fails. Value is a string error message.

### Diffing

Use cases:
- The primary use-case is simple feature branches. Imagine a user branches from main a few days ago to work on users-feature-branch. Co-workers continue to merge their PRs into main, which the user pulls down. User makes several commits to users-feature-branch. They then run `check_spec_conformance`. The diff generated is between their current files and the branch's effective comparison base against its intended upstream.
- Rebases matter. If the user rebases their feature branch onto newer `main`, treat that as equivalent to recreating the branch from newer `main` and replaying their branch commits. After the rebase, the effective comparison base moves forward to the branch's current fork-point with `main`.
- If the user branches off of their own feature branch at and makes users-feature-branch-subbranch, then the diff is between the on-disk state and the point at which they branched off of users-feature-branch to make users-feature-branch-subbranch.
- Comparison base selection and changed-path discovery use `internal/gittools.HeuristicMergeBase` and `internal/gittools.ChangedPathsSince`.

### Presentation

- In progress: `Checking SPEC conformance`
- Complete: `Checked SPEC conformance`
- Direct package-check subagents have labels based on module-relative package dir.
- `SubagentFinalMessage` formats direct package-check final JSON into human-readable package-slot output. It never prints raw JSON.
- TUI in-progress rendering may show one stable slot per direct package-check subagent under `Checking SPEC conformance`, with each slot showing latest descendant event from that package subtree until terminal package result is available.
- TUI completion body is compact: summary counts plus any `postcheck_error` lines.
- Noninteractive completion body may include actual nonconformance details, because it cannot rewrite earlier lines.
- Raw `ToolResult.Result` remains machine-readable JSON.

## Public API

```go
const ToolNameCheckSpecConformance = "check_spec_conformance"

type CheckSpecConformanceToolOptions struct {
	AgentInvoker   toolsetinterface.AgentInvoker
	Model          llmmodel.ModelID
	MaxConcurrency int // 0: use default concurrency
}

// NewCheckSpecConformanceTool creates a tool that checks SPEC.md conformance for packages in the current module and records conforming packages in CAS.
//
// authorizer should be a sandbox authorizer, not a package-jail authorizer.
func NewCheckSpecConformanceTool(authorizer authdomain.Authorizer, options ...CheckSpecConformanceToolOptions) llmstream.Tool
```
