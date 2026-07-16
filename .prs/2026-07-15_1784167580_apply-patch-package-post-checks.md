# PR

## User Summary (do not modify)

Make package-mode `apply_patch` post-edit diagnostics and lints consistently check the selected Go package.

Automatic post-edit feedback is a core part of package mode. A successful patch should tell the agent whether its Go changes compile and should run the configured fix-mode lints for the package, even when one patch changes several files or includes supporting files such as fixtures and `testdata`.

The current implementation infers the check target from the parent directories of changed files. It silently skips all post-edit checks when a patch spans more than one directory. When a patch changes only a nested supporting file, it runs checks against that nested directory rather than the selected package. This makes check behavior depend on how files happen to be distributed within a package code unit and can omit expected formatting, linting, and diagnostic feedback.

Fix package-mode `apply_patch` so configured post-edit lints run against the selected package after every successful patch, regardless of how many included directories changed. Diagnostics should compile the selected package when the patch changes Go code. The tool result should continue to include the resulting diagnostic and lint output for the agent.

## Plan [DONE]

### Package `internal/tools/coretools` [DONE]

- Give `apply_patch` post-check configuration an explicit target directory instead of inferring one from changed paths.
- After every successful patch, run configured fix-mode lints for that target.
- Run diagnostics first when at least one changed path is a Go source file.
- Preserve appended diagnostic/lint output and post-check error reporting.
- Keep single-file `edit` and `write` post-check behavior unchanged.
- Implement the contract documented in `internal/tools/coretools/SPEC.md`.

### Package `internal/agentbuilder` [DONE]

- Configure package-mode `apply_patch` post-checks with the selected `GoPkgAbsDir`, including default lint steps.
- Verify `apply_patch` checks use the selected package for multi-directory and nested supporting-file patches.
- Implement the contract documented in `internal/agentbuilder/SPEC.md`.

### Package `internal/noninteractive/integration` [DONE]

- Update the `pm-lints` HTTP replay to expect lint-only post-check output for its `SPEC.md`-only patch.
- Run the full project test suite.

## Review

Pending.

## Summary

Pending.

## State

- Branch: `jn/apply-patch-package-post-checks`
- Root cause: shared `coretools.runPostChecks` derives parent directories from changed paths and skips checks when more than one directory is present.
- Package-mode selected target is available as `toolsetinterface.Options.GoPkgAbsDir`.
- `edit`/`write` use the same shared post-check helper; this PR intentionally changes only `apply_patch` targeting semantics.
- Implementation commit `10cee73` adds explicit `ApplyPatchPostChecks.TargetDir`, conditional Go diagnostics, unconditional configured lints, package-mode wiring, and focused tests.
- Integration commit `9c2c2a4` updates `pm-lints` request and event replays for lint-only output after a `SPEC.md` patch.
- `go test ./...` passes after the replay update.
