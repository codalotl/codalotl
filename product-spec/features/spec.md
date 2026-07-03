# SPEC.md

Codalotl treats `SPEC.md` files in Go packages as the control surface for Go package behavior. These are central to the workflows and value proposition of Codalotl.

- A `$spec-md` skill exists that agents can use to understand `SPEC.md`.
- A fundamental goal of the system is ensuring the consistency of `SPEC.md` files and their package.
- To that end, a common operation is checking "spec conformance" - making sure a package conforms to a spec.
- One aspect of spec conformance is ensuring the Public API described in the `SPEC.md` file matches the implementation.
- The orchestrator workflow is tightly coupled to carefully modifying `SPEC.md` files as part of the PR workflow:
    - Propose `SPEC.md` changes.
    - A separate agent reviews them and suggests feedback. Iterate.
    - When the PR is nearly done, a review phase checks that the package conforms.

## CLI

### codalotl spec fmt <path/to/pkg_or_SPEC.md>

Formats Go code blocks in a package `SPEC.md`.

The command accepts a package path or direct `SPEC.md` path.

`spec fmt` is also used by lints after package-mode edits.

### codalotl spec diff <path/to/pkg_or_SPEC.md>

Compares the public API described in `SPEC.md` with the package implementation.

The command accepts a package path or direct `SPEC.md` path.

Output is intended for humans and agents. If no differences are found, no diff output is printed.

Diffs focus on public API shape and doc comments. They do not decide full product or behavior conformance.

### codalotl spec ls-mismatch <pkg/pattern>

Lists packages whose `SPEC.md` public API differs from implementation.

The package argument may use Go package-pattern style, including `./...`.

Packages without both a valid Go package and a `SPEC.md` are not listed.

### codalotl spec status

Prints per-package `SPEC.md` status across discovered repo modules.

Status includes:
- whether package has `SPEC.md`
- whether public API matches implementation
- whether implementation has current spec-conformance certification

`spec status` is a repo navigation tool. It helps users decide which packages need spec work, implementation work, or conformance review.

## Conformance

Public API match is mechanical and narrow.

Behavior conformance is checked by `check_spec_conformance` and recorded through CAS when packages conform.

`codalotl spec status` may combine both signals so users can distinguish:
- missing spec
- public API drift
- missing or stale conformance certification

## Lints

Codalotl can run spec checks as package lints:
- `spec-fmt`: fixes `SPEC.md` formatting.
- `spec-diff`: checks public API drift.

These lints keep package-mode edits aligned with the package spec without requiring users to run spec commands manually.
