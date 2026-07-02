# Lints

Codalotl has a lint system that detects and fixes formatting, documentation, and static-analysis issues in Go code and SPEC.md files. This system is user configurable to allow them to select which lints they'd like to run (including custom lints). Example lints: `gofmt`; `staticcheck`; etc.

The lints run in package mode and are package-centered. A lint run targets one Go package and reports whether the configured checks passed, fixed issues automatically, or found issues that still require code changes.

## When Lints Run

Codalotl runs lints in package mode, when:
- After edits: when a package-mode editing tool changes files, Codalotl runs post-edit lints. Lints that can safely fix files may do so automatically. This is done in the same tool call as the edit, with the result included in the tool call output.
- Initial package context: when a package-mode session starts, Codalotl checks enabled lints and includes the status in the package context. These checks do not edit files.
- Explicit lint fixing: the agent can use the `fix_lints` tool when it wants to clean up lint issues directly.
- Test verification: after `run_tests` runs package tests, Codalotl runs verification lints so the final result covers both tests and configured package checks.

This gives the agent early warning about existing problems, fast feedback after its own changes, and a dedicated way to clean up issues before handing work back to the user.

## Modes

Lints run in one of two user-visible modes:

- Check mode reports whether issues exist without editing files.
- Fix mode runs automatic fixers where available, then reports anything that remains.

Initial context and test verification use check mode. Post-edit linting and `fix_lints` use fix mode.

A lint that cannot fix its own findings may still run during a fix-mode lint pass. In that case it reports remaining issues for the agent to address manually.

## Built-In Lints

The default lint set is intentionally low-noise and Go-focused:

- `gofmt`: checks and fixes Go formatting.
- `spec-fmt`: formats package `SPEC.md` files when applicable.
- `spec-diff`: checks whether a package with `SPEC.md` still matches its implementation during test and explicit lint-fix workflows.

Additional preconfigured lints can be enabled in config:

- `reflow`: normalizes Go documentation comment wrapping.
- `staticcheck`: runs static analysis.
- `golangci-lint`: runs the user's configured golangci-lint checks.

Users can also configure custom lint steps for repository-specific checks.

## Results

Lint results are returned to the LLM, usually in the tool call that initiated them.

Lint results are sometimes displayed to the user (in a summarized form), but are sometimes not:
- Editing files does not display lint pass/fail information.
- Running tests (with `run_tests`) does display a Lint pass/fail overview.

## Configuration

Users configure lints through normal Codalotl config, described in `features/config.md`.

Configuration lets users:

- Keep the default lint set.
- Add preconfigured lints like `reflow`, `staticcheck`, or `golangci-lint`.
- Replace the default lint set with repository-specific checks.
- Disable linting entirely.
- Limit individual lints to particular situations, such as only running slower checks during explicit lint fixes or test verification.

The recommended user experience is to keep automatic linting fast and low-noise. Slow or high-noise checks should usually be reserved for explicit fixing or test verification, where the user expects a more complete validation pass.
