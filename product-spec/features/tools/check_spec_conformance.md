# `check_spec_conformance`

`check_spec_conformance` lets an orchestrator or agent check whether Go packages conform to their colocated `SPEC.md` files and records successful checks in CAS.

## Availability

- Available to PR-orchestrator-style agents through the spec toolset.
- Available in workflows that can launch package-focused subagents.
- Intended for Go modules in a git-backed sandbox.

## Behavior

- The tool checks Go packages in the current module.
- A package is checkable only when it has a `SPEC.md` file in the package directory.
- If explicit packages are supplied, only those packages are considered.
- If explicit packages are not supplied, packages without `SPEC.md` are skipped and packages with an up-to-date CAS record asserting `conforms=true` are skipped.
- Explicit packages bypass cached-conformance skipping so the agent can force a recheck.
- Invalid explicit packages fail the tool before package checking starts.
- Explicit packages that do not have `SPEC.md` fail the tool before package checking starts.
- `only_changed=true` further restricts checking to packages whose package scope changed against the current git comparison base.
- If no packages are eligible after filtering, the tool returns an empty JSON object.
- For each eligible package, the tool launches one package-focused subagent, with bounded concurrency.
- Each package-check subagent is labeled with the module-relative package directory.
- Package-check subagents receive package-mode context, the package diff against the comparison base, and programmatic SPEC/public-API diff context.
- Subagents identify conformance or nonconformance. When a package is nonconforming, the workflow asks for follow-up analysis so orchestrators can decide whether to fix code, fix the spec, or treat the issue as out of scope.
- Package checking is read-only in intent, but the overall tool may write CAS records after package verdicts.
- Once package checking starts, package-scoped failures are reported inside the raw JSON result instead of failing the whole tool call.
- Overall tool-call failures are reserved for parameter, module-loading, git-baseline, authorization, or other pre-launch/global failures.

## Package Scope

- Package keys in results are module-relative package directories.
- The root module package is represented by its module-relative package key.
- Package scope follows the normal Go package code unit rooted at the package directory.
- The same package scope is used for package filtering, changed-path attribution, subagent authorization, and CAS conformance reuse or invalidation.
- Nested Go packages are not part of the parent package scope.
- `testdata` and non-Go supporting files are considered according to the package code-unit rules used by package mode and CAS.

## Inputs

- `only_changed`: required boolean. When true, only packages whose scoped contents changed since the git comparison base are checked.
- `packages`: optional array of package selectors. Entries may be current-module import paths or module-relative package paths.

## Output

The tool returns raw machine-readable JSON keyed by module-relative package directory. Only packages that were checked appear in the result.

Example:

```json
{
  "internal/foo": {
    "conforms": true
  },
  "internal/bar": {
    "conforms": false,
    "nonconformances": [
      {
        "severity": "major",
        "latent": false,
        "message": "The implementation accepts nil where SPEC.md requires an error.",
        "analysis": "The current branch introduced the behavior change, so the orchestrator should fix code or update SPEC.md intentionally."
      }
    ]
  },
  "internal/baz": {
    "error": "timed out"
  },
  "internal/qux": {
    "conforms": true,
    "postcheck_error": "store CAS conformance: permission denied"
  }
}
```

Result fields:

- `conforms`: true when the package conforms; false when it does not conform.
- `nonconformances`: present for nonconforming packages and contains one or more issues.
- `severity`: one of `trivial`, `minor`, or `major`.
- `latent`: true when the issue predates the comparison base; false when the current diff introduced it.
- `message`: human-readable nonconformance explanation.
- `analysis`: human-readable follow-up analysis for orchestrator decision-making.
- `error`: package-scoped failure before a valid verdict was produced.
- `postcheck_error`: package-scoped failure after a valid verdict, such as a CAS write failure.

For conforming packages, `nonconformances` is omitted. For nonconforming packages, `nonconformances` contains at least one issue. `postcheck_error` may appear together with a valid conforming or nonconforming verdict.

## CAS

- When a checked package conforms, the tool writes a CAS record asserting `conforms=true` for that package's current content.
- Nonconforming results are not stored as CAS conformance records.
- A nonconforming recheck clears or invalidates any matching cached `conforms=true` state for the same current package content.
- CAS records are expected product artifacts and may be committed with PR work.
- CAS write failures do not erase a valid package verdict; they are reported as `postcheck_error`.

## Presentation

Human-facing output uses an append-style presentation because checks may run multiple subagents.

In progress:

```text
• Checking SPEC conformance
```

Completion:

```text
• Checked SPEC conformance
  └ 2 conforming, 1 non-conforming, 1 error
```

The TUI may show stable per-package slots under the in-progress line while subagents run. Direct package-check subagent final JSON is formatted into package-level status, not printed raw. Human presentation should summarize counts and visible errors, including `postcheck_error` lines, but it should not display the hidden `analysis` text by default.

## Permissions

- The tool reads module, package, SPEC, git, and CAS state needed to select packages and prepare package checks.
- Package-check subagents receive package-scoped authorization.
- CAS writes are authorized before records are written.
- CAS storage may live outside the sandbox according to the CAS feature rules.
