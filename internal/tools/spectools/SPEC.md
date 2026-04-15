# spectools

Tools for `SPEC.md` workflows.

## check_spec_conformance

This tool checks Go packages in this module for `SPEC.md` conformance. It accepts one parameter: `only_changed`. It returns JSON that indicates which checked packages conform and which do not. It has the side-effect of automatically doing CAS writes for any conforming package.

Details:
- Skips packages without `SPEC.md`.
- Skips packages whose CAS record already says `conforms=true`.
- `only_changed=true` further restricts to packages whose on-disk state changed against the current git comparison base. See `### Diffing`
- `only_changed=false` checks all current-module packages that have `SPEC.md` and do not already have CAS `conforms=true`.
- If no packages are eligible, tool returns `{}`.
- Runs one `limited_package_mode` subagent per package, with bounded concurrency.
- Supplies package diff and programmatic `spec diff` context to each subagent.
- Subagent instructions remain read-only in intent and use the `$spec-md` check-conformance workflow.
- Tool result is raw JSON keyed by module-relative package dir. Only checked packages appear.
- If a checked package has no diff against the comparison base, any reported nonconformance is `latent=true`.
- CAS writes:
    - Happen for a package as soon as that package's result comes in.
    - write `conforms=true` only for conforming packages
    - do not store nonconforming results
- If a failure or error happens outside of a concurrent subagent: overall tool call fails
- If a failure or error happens inside a subagent: record error in package's object. Do not fail overall tool call.
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
- `severity`: `trivial`, `minor`, or `major`
- `latent`: `true` when issue predates comparison base; `false` when current diff introduced it
- `message`: human-readable explanation
- `error`: only if subagent experiences error. Value is a string error message.

### Diffing

Use cases:
- The primary use-case is simple feature branches. Imagine a user branches from main a few days ago to work on users-feature-branch. Co-workers continue to merge their PRs into main, which the user pulls down. User makes several commits to users-feature-branch. They then run `check_spec_conformance`. The diff generated is between their current files and the point at which they branched off of main a few days ago.
- If the user branches off of their own feature branch at and makes users-feature-branch-subbranch, then the diff is between the on-disk state and the point at which they branched off of users-feature-branch to make users-feature-branch-subbranch.
- If the user is on main/master, the diff is ONLY against their uncommitted files. "What would a simple `git diff` show".

### Presentation

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
	MaxConcurrency int // 0: use default concurrency
}

// NewCheckSpecConformanceTool creates a tool that checks SPEC.md conformance for packages in the current module and records conforming packages in CAS.
//
// authorizer should be a sandbox authorizer, not a package-jail authorizer.
func NewCheckSpecConformanceTool(authorizer authdomain.Authorizer, options ...CheckSpecConformanceToolOptions) llmstream.Tool
```
