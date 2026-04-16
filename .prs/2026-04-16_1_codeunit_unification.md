# PR

## User Summary (do not modify)

Problem/Background:
- We currently have multiple similar-but-not-identical definitions of "what is part of a package":
  - generic CAS package identity in `internal/gocas`
  - package-mode filesystem scope / jail
  - `check_spec_conformance`'s notion of what changes count as package changes
- These definitions are not interchangeable, but today they are also not clearly named or intentionally separated. Some of the current behavior appears to be drift / accidental duplication rather than deliberate design.
- In practice, package-mode and `check_spec_conformance` are trying to reason about a broader subtree-based package surface than `gocas.StoreOnPackage` does today.
  - Example: package-mode includes supporting files in the package subtree such as `data/`, reachable `testdata/`, markdown, YAML, etc.
  - Example: `internal/tools/spectools/check_spec_conformance` already treats package-local support files as relevant to whether a package should be re-checked.
  - Example: `internal/agentbuilder` behavior is materially affected by `internal/agentbuilder/data/config.yml` and other embedded files under `internal/agentbuilder/data/`, even though those files are not Go files and are therefore outside current `gocas.StoreOnPackage` identity.
- This mismatch creates real conceptual and correctness problems:
  - We have duplicate implementations of package-mode-ish subtree logic in multiple places.
  - `check_spec_conformance` can inspect a broader surface than its CAS key currently captures.
  - It is too easy for future changes to accidentally widen or narrow one definition without updating the others.

Goal:
- Introduce a shared default code-unit constructor for Go-package subtree work:
  - `codeunit.DefaultGoCodeUnit(absBaseDir string) (*codeunit.CodeUnit, error)`
- Extend `internal/gocas` with code-unit-based CAS APIs:
  - `StoreOnCodeUnit`
  - `RetrieveOnCodeUnit`
- Use the shared default code unit for:
  - package-mode filesystem scope / jail
  - `internal/tools/spectools/check_spec_conformance`
- Keep `StoreOnPackage` / `RetrieveOnPackage` as the narrower existing API for cases that truly want "Go package identity" rather than the broader subtree surface.

Motivation / Design intent:
- I am **not** trying to force the repo into "one universal definition of package" for every purpose.
- I **am** trying to give us one explicit, shared default definition for the common subtree-oriented concept that several systems are already independently approximating.
- The important design distinction I want the implementation to preserve is:
  - `StoreOnPackage`: narrow Go-package identity
  - `DefaultGoCodeUnit`: default package workspace / subtree surface
  - `StoreOnCodeUnit`: CAS keyed by that broader subtree surface
- The reason to unify package-mode and `check_spec_conformance` around `DefaultGoCodeUnit` is that both are already fundamentally about that broader subtree surface.
- The reason to add `StoreOnCodeUnit` is that tools such as `check_spec_conformance` should be able to key CAS off the same surface they are allowed to inspect and reason over.

Proposal:
- Add `codeunit.DefaultGoCodeUnit(absBaseDir)` with semantics ~equivalent to the current package-mode default behavior, but centralized in one place.
- Intended semantics of `DefaultGoCodeUnit`:
  - include the base dir and direct files in it
  - recursively `IncludeSubtreeUnlessContains("*.go")`
  - include reachable `testdata` directories
  - `PruneEmptyDirs()`
  - exclude descendant directories whose basename starts with `.` (i think this part is new?)
- The dot-dir exclusion is important because package-mode / conformance scope should not accidentally absorb repo metadata such as `.codalotl` or other hidden control directories just because they live under a package subtree and happen not to contain Go files.
- Add `gocas.StoreOnCodeUnit` / `RetrieveOnCodeUnit` that hash the files included by a `codeunit.CodeUnit`.
  - Path names used in the hash should still be relative to `gocas.DB.BaseDir`, not to `unit.BaseDir()` -- same decision as exiting CAS, and changing it is out of scope for now.
- Convert package-mode to use `codeunit.DefaultGoCodeUnit` instead of open-coding the current sequence.
- Convert `check_spec_conformance` to use the same `DefaultGoCodeUnit` for:
  - the subagent authorizer scope
  - changed-path attribution / package diff scope
  - CAS invalidation for conformance records, via `StoreOnCodeUnit` rather than package-only CAS

Non-goals:
- Do not delete or repurpose `StoreOnPackage` / `RetrieveOnPackage`.
- Do not claim that every tool in the repo should use `DefaultGoCodeUnit` or `StoreOnCodeUnit`.
- Do not try to solve every possible tool-specific invalidation problem in this PR.
  - Some tools may legitimately depend on files outside the package subtree, such as `go.mod`, and that is a separate design question.
- Do not overfit this to `.codalotl` specifically. The intended rule is hidden descendant dirs in general, not a one-off hard-coded repo name list.

## Plan

### Phase 0

#### Package `internal/codeunit` [DONE]
- Add shared constructor `DefaultGoCodeUnit(absBaseDir string) (*codeunit.CodeUnit, error)` for subtree-oriented Go package work.
- Semantics:
  - include base dir and direct files in it
  - recursively include descendant dirs unless that dir contains `*.go`
  - include reachable `testdata` subtrees even when they contain `*.go`
  - prune empty dirs
  - exclude descendant dirs whose basename starts with `.`
- This is the shared default for package workspace / subtree scope. It does not replace more specific or narrower code-unit construction when a caller wants something else.

#### Package `internal/gocas`
- Add `StoreOnCodeUnit` / `RetrieveOnCodeUnit`.
- Hash included files from a `codeunit.CodeUnit`, with path names still interpreted relative to `gocas.DB.BaseDir`.
- Keep `StoreOnPackage` / `RetrieveOnPackage` as the narrower Go-package API.

#### Package `internal/gocas/casconformance`
- Keep the package-shaped public API.
- Re-key conformance records using the default Go code unit rooted at the package dir so conformance caching matches `check_spec_conformance` scope.

### Phase 1

#### Packages `internal/tui`, `internal/noninteractive`, `internal/agentbuilder`
- Replace open-coded package-mode code-unit construction with `codeunit.DefaultGoCodeUnit`.
- Preserve the existing broader subtree behavior, while picking up the explicit hidden-dir exclusion.

#### Package `internal/tools/spectools`
- Use `codeunit.DefaultGoCodeUnit` for:
  - package authorizer scope
  - package changed-path attribution / diff scope
  - conformance CAS reuse / invalidation
- Keep any extra deleted-path handling needed so changed-path attribution still works when paths no longer exist on disk.

### Possible follow-up

#### Package `internal/tools/pkgtools`
- Evaluate whether other package-targeted jails such as `clarify_public_api` should also switch to `DefaultGoCodeUnit`.
- This is not required to satisfy the user request; only do it in this PR if it is obviously the same concept and stays low-risk.

## Decisions

- Guaranteed scope for this PR: package-mode jails plus `check_spec_conformance`.
- Other package-targeted jails are opportunistic cleanup, not required for completion.

## Review

- Pending.

## Summary

## State

- `internal/codeunit` now has `DefaultGoCodeUnit`; tests cover reachable `testdata`, hidden descendant dirs, empty-dir pruning, and nested-package exclusion.
- Current duplicate Go-package subtree builders:
  - `internal/noninteractive/noninteractive.go`
  - `internal/tui/session.go`
  - `internal/agentbuilder/yaml.go`
  - `internal/tools/pkgtools/clarify_public_api.go`
  - `internal/tools/spectools/check_spec_conformance.go`
- `internal/tools/spectools` already has extra diff-scope logic for deleted/nonexistent paths via `blockedSubtrees`; likely keep that and swap only the on-disk scope builder.
- `internal/gocas/casconformance` public API is package-shaped today and can likely keep that shape while changing its internal CAS keying.
