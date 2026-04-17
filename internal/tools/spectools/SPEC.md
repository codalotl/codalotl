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
- Package-scoped failures include subagent errors and per-package preparation, parsing, or CAS-write failures.
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
}
```

Notes:
- `conforms=true` omits `nonconformances`; explicit `null` is equivalent to omission
- `conforms=false` includes one or more `nonconformances`
- Any other result-shape combination is invalid and is treated as a package-scoped error
- `severity`: `trivial`, `minor`, or `major`
- `latent`: `true` when issue predates comparison base; `false` when current diff introduced it
- `message`: human-readable explanation
- `error`: only if package checking for that package fails after launch. Value is a string error message.

### Diffing

Use cases:
- The primary use-case is simple feature branches. Imagine a user branches from main a few days ago to work on users-feature-branch. Co-workers continue to merge their PRs into main, which the user pulls down. User makes several commits to users-feature-branch. They then run `check_spec_conformance`. The diff generated is between their current files and the branch's effective comparison base against its intended upstream.
- Rebases matter. If the user rebases their feature branch onto newer `main`, treat that as equivalent to recreating the branch from newer `main` and replaying their branch commits. After the rebase, the effective comparison base moves forward to the branch's current fork-point with `main`.
- If the user branches off of their own feature branch at and makes users-feature-branch-subbranch, then the diff is between the on-disk state and the point at which they branched off of users-feature-branch to make users-feature-branch-subbranch.
- Comparison base selection and changed-path discovery use `internal/gittools.HeuristicMergeBase` and `internal/gittools.ChangedPathsSince`.

### Presentation

- In progress: `Checking SPEC conformance`
- Complete: `Checked SPEC conformance`
- Uses `SubagentEventPolicySummarizeBySubagent`.
- Each package-check subagent is identified in the TUI by its existing subagent label, which is the module-relative package dir.
- While running, the TUI shows one visible package row per active subagent under the parent tool call and updates that row with the latest descendant event.
- When a package check finishes, that package row shows the per-package result: conforming, non-conforming with issues, or package-scoped error.
- Completion body summarizes conforming, non-conforming, and errored package counts only.
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
