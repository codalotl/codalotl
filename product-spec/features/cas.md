# CAS

CAS stands for "content-addressable storage" - in this product, it's a system to store metadata attached to content hashes (typically Go packages). For example, we might flag that a certain Go package - its current files, paths, and bytes - has been analyzed for security vulnerabilities, with no vulnerabilities found. As soon as the package is edited, the analysis is implicitly invalidated (the hash changes).

Tools like `check_spec_conformance` and `refactor` will often automatically create CAS files.

## Example

One operation Codalotl does regularly is checking "spec conformance" (via a `check_spec_conformance` tool) - making sure a package's Go code conforms to the SPEC.md in the same dir. When it finds the code does conform to the SPEC.md, it writes a CAS entry against the package's hash, recording this fact.

Concretely, a filename like this is written:
`.codalotl/cas/specconforms-1/0a/07c31ddb15f6d14e7a17f46dfb04bbba24443d484c437db96c87a5c147789f`

Which contains a JSON object like this:
```json
{"kind":"cas-record-v1","metadata":{"conforms":true},"additional_info":{...}}
```

## Hash mode

A Go package can be hashed in two ways:
- .go files and SPEC.md
- .go files and ~all other files in the dir, recursively, up to but not including nested Go packages.

The former can be used for code-only concerns; the latter can be used when supporting files might play a role.

## Merge Conflicts

One very important property of this system is to be resilient to merge conflicts in multi-user repos. As such, we avoid index files. In this system, there should be almost no merge conflicts even when engineers modify the same package.

## CAS files

- If the nearest `.git` repo is located in `$GIT_ROOT` (recursively looking in cwd, parent, ...), the root CAS dir is `$GIT_ROOT/.codalotl/cas`. This can be overridden with `$CODALOTL_CAS_DB`.
- Allowed to be outside the sandbox dir.

## Checked into git

- These CAS files are intended to be checked into git. If `$CODALOTL_CAS_DB` is defined and outside the git repo (or git-ignored), the behavior is undefined:
    - Storing CAS files outside the repo should work.
    - But some workflow items might break - for instance, the agent might use `git status` to notice new files, expecting to find CAS files.

## Namespaces and Versions

Each type of metadata has its own namespace. Ex: `specconforms`; `docs-fix`. Similarly, each namespace is versioned, affording us the ability to bump the version, invalidating all existing CAS records.

Each namespace "knows" its associated current version and a hash mode.

## Determining Churn and Age

To determine churn %: we need to find a commit so we can diff a package against another known version. We know we have the right commit if the package hash at that commit matches the hash of the CAS record. If we cannot find a commit, we cannot calculate churn.

The most likely way to find the commit is the commit that added the CAS record. Metadata within a CAS record may also be used (we store some git data there).

Age is based on the time of the commit that added the CAS entry, falling back to the mtime of the file.

## CLI

The CLI offers commands to view and manipulate CAS records. Namespace parameters from the CLI refer to the non-versioned namespace.

The CAS files are found using the rules in `## CAS files`, even if outside the sandbox dir or in parent dirs.

### codalotl cas get <namespace> <path/to/pkg>

Prints CAS record if it exists, otherwise exits with status 1.

### codalotl cas ls-namespaces

Lists namespaces and their current version of all CAS types in the codebase (not which records have been saved so far). Does not display hash mode.

### codalotl cas ls-stale <namespace> [--stale-after-days=30] [--min-churn-percent=20]

Lists packages (one per line, prefixed with `.`) that have no CAS file for their hash for the namespace. Only lists Go packages (not dirs with no .go files). Packages listed are based on the git repo (see `## CAS files`), and are relative to the git repo.

If `--stale-after-days=N` is present, list only packages whose most recent known CAS-covered state became stale at least N days ago.

If `--min-churn-percent=N` is present, list only packages whose current content differs from the most recent CAS-covered state by at least N%. Packages without churn metrics are not included (e.g., we couldn't determine a corresponding git commit).

If both are passed, the conditions are OR'ed. Note that packages that have never had a CAS entry are always included. If a Go package has no CAS record at all, for any hash, it is always considered stale and is always returned.

NOTE: there's a lot of nuance and edge cases in the above. These are implementation details.

### codalotl cas prune [--days=N]

Deletes CAS files:
- prior versions (if a namespace bumps the version).
- CAS files older than N days (default: 30) AND where a newer CAS entry also exists for the (namespace, package) pair.

### codalotl cas recertify <path/to/pkg> --namespaces="<namespace1>[,<namespace2>,...]"

Recertify asserts that a package's current files wrt the namespace are compliant with a recent CAS record.
- `--namespaces` is required with at least one namespace. It's a comma-separated list of namespaces.
- no-op for (pkg, namespace) pairs where hash is already up-to-date.
- Writes a new CAS entry with the ~same content as the most recent one, but with some updated metadata/additional_info, when appropriate (ex: updated git SHAs; updated file lists).
    - New CAS entry has extra metadata indicating it's a recertification: `"recertified": true, "recertified_from_hash": "...", "recertified_from_record": "..."`
- Never deletes or mutates existing CAS entries.
- Before recertification, check invariants (things like same version, hash mode, package name, etc), raising errors as approprate. Display warnings if high-risk things are being done (ex: recertification done in different branches; large churn %; recertifying very old record; etc).

Problem this solves: agent runs multiple refactors on a package in a row: `dry`, `test-cleanup`, `test-ensure-coverage`, `docs-fix`. Each one writes a CAS entry, and each one invalidates the previous entry. We need a way for the agent to say, "all these refactors are still valid." In theory, a refactor could break a previous refactor. In practice, that's rare. For refactors where that matters a lot, just don't recertify it.

### codalotl cas ls-summary <namespace> [--csv]

Displays a tabular summary of all packages in the system with respect to the namespace. If `--csv` is used, instead of printing a pretty table for display in a terminal, prints a CSV.

Columns:
- Package
- CAS (either `yes` or `no`, for is there a CAS entry matching current package's hash)
- Prev CAS (either `yes`, `no`, or `-`, for is there a CAS entry for this package that has been invalidated. If CAS in prior column is `yes`, this is `-` for "does not matter")
- Age (either `-` if N/A, or something like `17d` indicating a CAS record was saved 17 days ago)
- Churn % (either `-` if N/A, or something like `18%` indicating the package had 18% of lines changed relative to when the prior CAS record was saved).

## Future Design Possibilities

The following are NOT part of the spec, but simply ideas to explore IF certain problems occur:
- We may want stronger recertify semantics:
    - namespaces opt-in or opt-out to recertification
    - expose recertification more easily, including in ls-summary
    - prune should preserve provenance chains
- After some experimentation with this system, I think I'd like to unify ls-summary and ls-stale:
    - add flags to filter in various ways, including by pkg name.
    - it may be worth a performance pass. this was a bit slow at this relatively low scale. My guess is commit to determine churn.
