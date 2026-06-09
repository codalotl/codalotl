# `get_usage`

`get_usage` finds usages of an identifier. It's typically called from a package-mode agent which needs to identify downstream usages - e.g., which other packages use this identifier (it also finds same-package usages).

## Inputs

- `defining_package_path`: Go package directory relative to the sandbox, or a Go import path.
- `identifier`: identifier defined in `defining_package_path`.

## Output

The tool returns an agent-facing usage summary with references to packages, files, line numbers, source lines, and selected code snippets when helpful.

Errors include invalid parameters, unresolved packages, package load failures, authorization failures for sandbox packages, and identifiers that are not defined by the target package.

Example output:

```text
--- References ---

internal/gocas/gocas.go
1018:        summary    *PackageRecordSummary // Summary contains the record metadata used for matching, ordering, and pruning.

internal/gocas/gocas.go
102:        Current *PackageRecordSummary

internal/gocas/gocas.go
105:        PriorInvalidated *PackageRecordSummary

internal/gocas/gocas.go
1062:    func (db *DB) recertificationWarnings(currentInfo cas.AdditionalInfo, source *PackageRecordSummary, records []*priorPackageRecord,
currentRelPaths []string) []string {

internal/gocas/gocas.go
1129:    func betterPackageRecord(candidate, incumbent *PackageRecordSummary) bool {

--- A handful of examples of usage ---

internal/gocas/gocas.go
func recordOlderThan(record *PackageRecordSummary, cutoff time.Time) bool {
    return record != nil && !record.Time.IsZero() && record.Time.Before(cutoff)
}

internal/gocas/gocas.go
func betterPackageRecord(candidate, incumbent *PackageRecordSummary) bool {
    if candidate == nil {
        return false
    }
    if incumbent == nil {
        return true
    }
    if candidate.Time.After(incumbent.Time) {
        return true
    }
    if incumbent.Time.After(candidate.Time) {
        return false
    }
    return candidate.Hash > incumbent.Hash
}
```

## Behavior

- The agent supplies the Go package that defines the identifier.
- The defining package may be a sandbox-relative package directory or a Go import path.
- The agent supplies one identifier defined by that package.
- Identifier forms include package-level functions, types, vars, consts, and methods such as `T.M` or `*T.M`.
    - FUTURE: `T.Field` where T is a struct type is NOT currently implemented, but would be valuable to do so.
- The tool resolves and loads the defining package, then finds references to that identifier from packages that use it.
- The result focuses on packages and files that use the selected or defining package, so the agent can reason about callers without broadly reading unrelated source.
- The result may include intra-package references when that helps explain how the identifier is used.

## Presentation

Example display:

```text
• Read Usage path/or/import/pkg Identifier
  └ Found 2 results.
```

## Permissions

Reads of packages inside the sandbox are authorized before usage information is generated.

Packages outside the sandbox that are resolved through Go's standard library or module dependency graph may be read as dependency context.
