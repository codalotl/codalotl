# CAS

CAS stands for "content-addressable storage" - in this product, it's a system to store metadata attached to content hashes (typically Go packages). For example, we might flag that a certain Go package - it's current files, paths, and bytes - have been analyzed for security vulnerabilities, with no vulnerabilities found. As soon as the package is edited, the analysis is implicitly invalidated (the hash changes).

Tools like `check_spec_conformance` and `refactor` will often automatically create CAS files.

## Example

One operation Codalotl does regularly is checking "spec conformance" (via a `check_spec_conformance` tool) - making sure a package's Go code conforms to the SPEC.md in the same dir. When it finds the code does conform to the SPEC.md, it writes a CAS entry against the package/spec's hash, recording this fact.

Concretely, a filename like this is written:
`.codalotl/cas/specconforms-1/0a/07c31ddb15f6d14e7a17f46dfb04bbba24443d484c437db96c87a5c147789f`

Which contains a JSON object like this:
```json
{"kind":"cas-record-v1","metadata":{"conforms":true},"additional_info":{...}}
```

## Package vs Code Unit

There's a couple options to hash against: Go package vs code unit. The Go package is only the .go files and SPEC.md, whereas the code unit is (roughly) a file tree located at a dir, up to but not including nested Go packages. So the code unit can include supporting non-package files and dirs.

## Merge Conflicts

One very important property of this system is to be resilient to merge conflicts in multi-user repos. As such, we avoid index files. In this sytem, there should be almost no merge conflicts even when engineers modify the same package.

## CAS files

- If the nearest `.git` repo is located in `$GIT_ROOT` (recursively looking in cwd, parent, ...), the root CAS dir is `$GIT_ROOT/.codalotl/cas`. This can be overriden with `$CODALOTL_CAS_DB`.
- Allowed to be outside of sandbox dir.

## Checked into git

- These CAS files are intended to be checked into git. If `$CODALOTL_CAS_DB` is defined and outside the git repo (or git ignored), the behavior is undefined:
    - Storing CAS files outside the repo should work.
    - But some workflow items might break - for instance, the agent might use `git status` to notice new files, expecting to find CAS files.

## Namespaces and Versions

Each type of metadata has its own namespace. Ex: `specconforms`; `docs-fix`. Similarly, each namespace is versioned, affording us the ability to bump the version, invalidating all existing CAS records.

Each namespace "knows" its associated current version and a hash mode (e.g., package vs code unit).

## CLI

The CLI offers commands to view and manipulate CAS records. Namespace parameters from the CLI refer to the non-versioned namespace.

The CAS files are found by `## CAS files`, even if outside sandbox dir or in parents' dirs.

### codalotl cas get <namespace> <path/to/pkg>

Prints CAS record if it exists, otherwise exits with status 1.

### codalotl cas ls-stale <namespace> [--stale-after-days=30] [--min-churn-percent=20]

Lists packages (one per line, prefixed with `.`) that have no CAS file for their hash for the namespace. Only lists Go packages (not code units with no .go files). Packages listed are based on the git repo (see `## CAS files`), and are relative to the git repo.

If `--stale-after-days=N` is present, list only packages whose most recent known CAS-covered state became stale at least N days ago.

If `--min-churn-percent=N` is present, list only packages whose current content differs from the most recent CAS-covered state by at least N%.

If both are passed, the conditions are OR'ed. Note that packages that have never had a CAS entry are always included.

NOTE: there's a lot of nuance and edge cases in the above. These are implementation details.

### codalotl cas prune

Deletes CAS files:
- prior versions (if a namespace bumps the version).
- CAS files where older than N days ago where a newer CAS entry also exists.