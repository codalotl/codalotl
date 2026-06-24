# `check_spec_conformance`

`check_spec_conformance` checks whether Go packages conform to their colocated `SPEC.md` files.

## Inputs

- `only_changed`: required boolean. When true, only packages whose scoped contents changed since the git comparison base are checked.
- `packages`: optional array of package selectors. Entries may be current-module import paths or module-relative package paths.

## Output

The tool returns raw machine-readable JSON keyed by module-relative package directory. Only packages that were checked appear in the result.

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

If no packages are eligible after filtering, the tool returns an empty JSON object.

Errors include invalid parameters, invalid explicit packages, packages without `SPEC.md` when explicitly requested, module-loading failures, git-baseline failures, authorization failures, and other pre-launch or global failures.

Example output:

```json
{
  "internal/llmstream": {
    "conforms": true
  },
  "internal/example": {
    "conforms": false,
    "nonconformances": [
      {
        "severity": "minor",
        "latent": false,
        "message": "SPEC.md says Parser.Parse accepts empty input, but Parse returns an error for empty input.",
        "analysis": "The implementation should either accept empty input or the SPEC should be revised before this package is recertified."
      }
    ]
  }
}
```

## Behavior

- The tool checks Go packages in the current module.
- A package is checkable only when it has a `SPEC.md` file in the package directory.
- If explicit packages are supplied, only those packages are considered.
- If explicit packages are not supplied, packages without `SPEC.md` are skipped and packages with an up-to-date CAS record asserting `conforms=true` are skipped.
- Explicit packages bypass cached-conformance skipping so the agent can force a recheck.
- `only_changed=true` further restricts checking to packages whose package scope changed against the current git comparison base.
- Package scope follows the normal Go package code unit rooted at the package directory. Nested Go packages are not part of the parent package scope.
- For each eligible package, the tool launches one package-focused subagent, with bounded concurrency.
- Package-check subagents receive package-mode context, package diff context, and SPEC/public-API diff context.
- Subagents identify conformance or nonconformance and include follow-up analysis for nonconforming packages.
- Package checking is read-only in intent, but conforming package results are recorded in CAS.
- CAS write failures do not erase a valid package verdict; they are reported as `postcheck_error`.
- Once package checking starts, package-scoped failures are reported inside the JSON result instead of failing the whole tool call.

## Presentation

Example display while running:

```text
• Checking SPEC conformance
  • internal/foo
    • Read internal/foo/file.go
  • internal/bar
    • Analyzing whether ...
```

Example display after completion:

```text
• Checking SPEC conformance
  • internal/foo
    • Conforms
  • internal/bar
    • Conforms
• Checked SPEC conformance
  └ 2 conforming, 0 non-conforming, 0 errors
```

- Each package runs its own subagent. Only the last event for that subagent (including subagent's subagents) is displayed for its package.
- When the subagent finishes, it's replaced with text like `Conforms` or `Does not conform`.
- When all targeted packages are finished, a summary line is printed with the counts of conforming, non-conforming, and errored checks.
