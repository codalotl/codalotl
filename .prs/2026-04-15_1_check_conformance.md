# PR

## User Summary (do not modify)

Problem/Background:
- Many Go packages have SPEC.md files, which are control planes and specifications for their Go code.
- The $spec-md skill (see internal/skills/default/spec-md/SKILL.md) has the concept of "checking conformance" - seeing if the Go code complies with the SPEC.md.
- We ALSO have the concept of "cas" - content-addressable storage. If we spend the LLM tokens to verify that a Go package conforms, we can record that fact.
    - This lets us keep track of which Go packages are known to conform, and which we need to re-check.
    - It also perhaps lets us determine the last known good state of a package, and then inductively determine that a changeset still conforms based on diffs to SPEC.md and Go code.
        - Ex: "it conforms at merge base, and we only added a single comment, so its conformance must not have changed."
- Since SPEC.md files are integral to our development system, it's imperative we have good hygiene around keeping them up-to-date and accurate. Otherwise the system breaks down and cannot be trusted.
- The tools and workflow to keep the CAS up to date with spec conformance checks are very manual and unwieldy today:
    - Run a CLI command to determine packages that don't have CAS-verified conformance.
    - Use package mode on packages one by one.
        - Type "$spec-md check conformance" (literally just that; it works reasonably well. The prompt is in the skill.)
        - Interpret what the agent says:
            - conforms: run a CLI command to set CAS on the package for spec conformance
            - doesn't conform: read the analysis from the agent, filter it through my human judgment
            - agent is wrong or nitpicky about an irrelevant thing: set CAS conformance anyway
            - agent is right: address the issue (sometimes update spec, sometimes update code, again based on my human judgment)
- We are exploring the "orchestrator" mode in codalotl - it is making many iterative commits, bringing a PR across the finish line.
    - It doesn't currently know about SPEC conformance or CAS at all

Goal:
- Make a new tool, "check_spec_conformance"
- Add the tool to the orchestrator agent's list of tools
- Nongoal: tell the orchestrator about this tool (I intend to manually tell the orchestrator to use it for now, until we prove it works well through experimentation)

Details:
- check_spec_conformance lives in package internal/tools/spectools (new package).
- Tool shape: check_spec_conformance(only_changed: true)
    - only_changed: true only checks conformance of packages that have a diff. So if only_changed=true and we're on a git branch, and we modify package internal/foo but not internal/bar, then internal/bar is not checked for conformance.
        - NOTE on diff: we check current state of files on disk vs X, where X is:
            - if we're on the main or master branch, we diff vs HEAD (e.g., current commit)
            - if we're on another branch, X is the point that the branch branched off of its parent. So for typical simple feature branches, it's the point we branched off of main/master.
            - Note to orchestrator: Decide and document exact semantics here in `## Decisions`.
- Tool output (ToolResult.Result):
    - JSON (described below)
- Tool side effects: any package that conforms should have its CAS set (this writes a file, in my case, to .codalotl/cas/...)
- The tool has a presenter that formats the JSON into nice human-digestible text (see review tool).
- does NOT check conformance for a package if there is no SPEC.md file in the package
- does NOT check conformance for any package that CAS says already conforms.
    - (and again further restricts that if only_changed is true).

Tool JSON output shape:

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
    }
}
```

NOTES on output:
- Allowed severities: `{"trivial", "minor", "major"}`
- latent=true indicates the non-conformance existed at the merge base. latent=false indicates the diff introduced the nonconformance
    - If there's no diff (ex: branch new feature branch), then everything checked will be latent=true.
    - If there's no diff and only_changed=true, then nothing is checked

More Details:
- This tool spins up **concurrent** subagents. Each subagent checks one package.
    - Limit concurrency to a configurable number (start with 5)
    - For now, we will accept that the TUI/etc will show interleaved results. This is not great. I have a plan to make this nicer. But out of scope for the PR.
- The subagent can just use an existing subagent like "limited_package_mode" for now (it's not read only. that's okay for v1)
- It is given the diff for its package automatically. Its package definition is: all the files in the folder, as well as anything the code unit jail typically allows (e.g., data, testdata, etc.)
    - If there is no diff, you decide what to do. Tell it about no diff, hide from it the latent distinction, or ???
- The agent should have its skill, so tell it the equivalent of "$spec-md check conformance". Not much more than that! Probably explain severity and latent, and tell it the output format.
- The subagent should be ~read-only. It's NOT critical to "lock this down". We can do that later. But the overall intention is that this is a read-only operation.
- We do have some automatic conformance checks.
    - Run the equivalent of `codalotl spec diff path/to/pkg` (don't shell out! do what it does) and supply to subagent as context before it starts.
- Figure out how to reconcile the instructions in the $spec-md skill for checking conformance and slightly overriding those instructions. Document in `## Decisions`

## Plan

### Phase 0

#### [DONE] Package `internal/tools/spectools`
- Create new package and `SPEC.md` for the tool.
- Add `check_spec_conformance` as a generic-mode tool with parameter `only_changed`.
- Tool scope:
  - enumerate Go packages in current module
  - skip packages without `SPEC.md`
  - skip packages whose CAS record already says `conforms=true`
  - when `only_changed=true`, further restrict to packages changed against the selected git comparison base
- Run one `limited_package_mode` subagent per package, with bounded concurrency.
- Pre-seed each subagent with:
  - package-local git diff vs comparison base
  - programmatic `spec diff` output for the package
  - strict output requirements for severity + latent classification
- Aggregate raw JSON results keyed by module-relative package dir.

#### [DONE] Package `internal/agentbuilder`
- Register the new built-in tool from `internal/tools/spectools`.
- Add `check_spec_conformance` to the `pr-orchestrator` tool list.
- Update existing registry/YAML coverage for the orchestrator toolset.

### Review follow-up [DONE]

#### [DONE] Package `internal/tools/spectools`
- Re-check CAS-skip eligibility so package-local non-Go support-file changes that matter to SPEC conformance still trigger a check.
- Restrict changed-path attribution to the package itself; do not treat descendant packages as changes to the parent package, and do not let root-package matching broaden to the whole repo.
- Enumerate packages using current-module semantics, excluding nested `go.mod` modules outside the current module.

## Review [DONE]

Review against `main` found actionable correctness issues in `internal/tools/spectools`; branch is not ready as-is.

### Accepted findings
- P1: CAS-verified packages are skipped too aggressively. `retrieveConformanceState` keys off package Go files plus `SPEC.md`, so edits in package-local support files such as `data/` or `testdata/` can leave a stale `conforms=true` record in place and wrongly suppress a re-check.
- P2: `pathInPackage` currently treats descendant package paths as belonging to the parent package. That can make `only_changed=true` check the wrong package, and package key `"."` broadens matching to the whole repo.
- P2: package enumeration uses recursive directory walking via `LoadAllPackages`, which can include Go packages from nested `go.mod` modules even though this tool is supposed to operate on the current module only.

### Additional review feedback
- P1: deriving package scope only from the current filesystem can miss package-local deletions. When `newPackageScope()` rebuilds scope without including paths that existed only at the comparison base, deleting support files such as `testdata/` content can make `only_changed=true` skip a changed package and leave a stale `conforms=true` CAS entry in place.
- P2: parent-branch inference does not currently accept remote-tracking creation messages such as `branch: Created from origin/main`. `parentBranchFromCreationMessage` only normalizes `refs/heads/...`, which makes branch-point selection spuriously ambiguous or fail outright on common `git switch -c feature origin/main` workflows.

## Summary

Add built-in `check_spec_conformance` support so the PR orchestrator can check `SPEC.md` conformance and record conforming packages in CAS.

- Add new package `internal/tools/spectools` with `check_spec_conformance`:
  - accepts `only_changed`
  - checks current-module packages with `SPEC.md`
  - skips already-conforming CAS entries when safe to do so
  - computes comparison-base-aware package diffs and precomputed spec-diff context
  - runs bounded concurrent `limited_package_mode` subagents
  - stores `conforms=true` in CAS for conforming packages
  - returns raw JSON results and a human-readable presenter summary
- Wire the tool into `internal/agentbuilder`:
  - register the built-in tool
  - expose it to `pr-orchestrator`
  - add focused registry/YAML coverage
- Address review findings in `internal/tools/spectools`:
  - do not skip CAS-verified packages when package-local support files changed
  - scope package diffs to the package plus support dirs like `data/` and `testdata`, excluding descendant Go packages
  - enumerate packages with current-module semantics so nested modules are excluded
- Add focused tests covering tool behavior, registry exposure, and the review regressions.

## Decisions

### Tool result keys
- Result JSON keys are module-relative package directories with slash separators, matching existing package display conventions (example: `internal/foo`).
- Only checked packages appear in the result.
- If no packages are eligible to check, the result is `{}`.

### Reconciling `$spec-md check conformance` with tool-specific output
- Each package subagent should still be told to use the `$spec-md` "check conformance" workflow.
- The outer tool precomputes and supplies the equivalent of `codalotl spec diff path/to/pkg`; the subagent should treat that as satisfying the skill's "run the fix lints tool" step unless it has a specific reason to distrust the context.
- The subagent is read-only for intent and must return strict JSON for one package:
  - `conforms`
  - optional `nonconformances`
  - `severity` constrained to `trivial|minor|major`
  - `latent` set to `false` only when the current diff introduced the issue

### `only_changed=false`
- `only_changed=false` means: check all current-module packages that have `SPEC.md` and do not already have CAS `conforms=true`.
- If a checked package has no diff against the comparison base, any reported nonconformance is `latent=true`.

## State

- Branch `jn/check-conformance-tool-2` now contains `internal/tools/spectools` implementation plus PR-file commits.
- New implementation package: `internal/tools/spectools`.
- `only_changed` uses actual-parent-branch semantics; ambiguity must fail explicitly rather than silently pick the wrong parent.
- Existing helpers likely useful:
  - `internal/lints.Run(..., spec-diff, ...)` for in-process spec-diff-style context
  - `internal/gocas/casconformance` for CAS writes/reads
  - `internal/agentbuilder/data/config.yml` for `pr-orchestrator` tool exposure
  - `internal/agentbuilder/genericTools()` for built-in tool registration
- `internal/tools/spectools` now contains `check_spec_conformance` implementation + tests.
- `internal/agentbuilder` now registers `check_spec_conformance` and exposes it to `pr-orchestrator`, with focused registry/YAML coverage.
- Review feedback is implemented in commit `6be56f8` (`spectools: fix package eligibility and scoping`).
- Additional review feedback remains to be actioned for deletion-aware package scoping and remote-tracking parent-branch inference.
