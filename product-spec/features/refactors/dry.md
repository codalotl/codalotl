# DRY Refactor

Codalotl has a `refactor` tool workflow to DRY up one Go package: `dry`.

It looks for worthwhile opportunities to share helpers and reduce duplicated package-local logic while preserving behavior.

## Behavior

The refactor may:
- create unexported helper funcs, vars, consts, or types
- replace repeated logic with helpers
- combine similar helpers when one helper stays clear
- simplify repeated test or non-test code when package-local

The refactor must:
- keep behavior unchanged
- keep changes package-local
- preserve public API
- avoid marginal similarity hunting

If a possible bug is noticed while refactoring, report it rather than fixing it.

## Workflow

`dry` runs as a limited package-mode refactor.

It should run package tests when appropriate and address lint issues surfaced by the environment.

When no worthwhile opportunity exists, the refactor should report no opportunity rather than making cosmetic churn.

## CAS

`dry` records successful package runs in CAS.

If current package contents already have a matching `dry` CAS record, broad refactor workflows may skip the package as already applied.

No-op runs can still be recorded, so repeated workflow passes do not keep reconsidering the same package.

## Orchestrator

The PR orchestrator can run `dry` through the `refactor` tool.

Users can create a refactor PR for `dry` with `codalotl pr refactor`, targeting one package or outdated packages across the repo.
