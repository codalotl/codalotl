# Lints

Codalotl runs lint checks as part of package-mode Go work so formatting, documentation, spec, and static-analysis feedback is visible while the agent is editing. Lints are meant to keep routine cleanup out of the user's prompt: the user should not need to remind the agent to run `gofmt`, check package docs, or verify common package health after every change.

Lint behavior is package-centered. A lint run targets one Go package and reports whether the configured checks passed, fixed issues automatically, or found issues that still require code changes.

## When Lints Run

Codalotl runs lints in package workflows where the result helps the user or agent make the next decision:

- Initial package context: when a package-mode session starts, Codalotl checks enabled lints and includes the status in the package context. These checks do not edit files.
- After edits: when a package-mode editing tool changes files, Codalotl runs post-edit lints. Lints that can safely fix files may do so automatically.
- Explicit lint fixing: the agent can use `fix_lints` when it wants to clean up lint issues directly.
- Test verification: after `run_tests` runs package tests, Codalotl runs verification lints so the final result covers both tests and configured package checks.

This gives the agent early warning about existing problems, fast feedback after its own changes, and a dedicated way to clean up issues before handing work back to the user.

## Modes

Lints run in one of two user-visible modes:

- Check mode reports whether issues exist without editing files.
- Fix mode runs automatic fixers where available, then reports anything that remains.

Initial context and test verification use check mode. Post-edit linting and `fix_lints` use fix mode.

A lint that cannot safely fix its own findings may still run during a fix-mode lint pass. In that case it reports remaining issues for the agent to address manually.

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

Lint results are shown as part of the workflow that ran them. The TUI and human-readable CLI output summarize the result.

When lints pass, Codalotl should keep the presentation compact. When lints fail or make edits, Codalotl should show enough detail for the agent and user to see what happened.

If no lints are configured, lint workflows succeed with a clear status rather than looking like a missing result.

## Configuration

Users configure lints through normal Codalotl config, described in `features/config.md`.

Configuration lets users:

- Keep the default lint set.
- Add preconfigured lints like `reflow`, `staticcheck`, or `golangci-lint`.
- Replace the default lint set with repository-specific checks.
- Disable linting entirely.
- Limit individual lints to particular situations, such as only running slower checks during explicit lint fixes or test verification.

The recommended user experience is to keep automatic linting fast and low-noise. Slow or high-noise checks should usually be reserved for explicit fixing or test verification, where the user expects a more complete validation pass.

## Relationship To Tools

`fix_lints` is the direct agent tool for lint cleanup. It runs configured lint fixes for a package and reports whether anything remains.

`run_tests` combines package tests with lint verification. A passing `run_tests` result means both tests and configured verification lints passed for that run.

Package-mode editing tools such as `apply_patch`, `edit`, and `write` run post-edit checks where practical. These checks may automatically format or otherwise clean up files before returning the tool result.
