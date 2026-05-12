# CAS

CAS stands for "content-addressable storage" - in this product, it's a system to store metadata attached to content hashes (typically Go packages). For example, we might flag that a certain Go package - it's current files and bytes - have been analyzed for security vulnerabilities, with no vulnerabilities found. As soon as the package is edited, the analysis is implicitly invalidated (the hash changes).

## CAS files

- If the `.git` repo is located in `$GIT_ROOT`, the root CAS dir is `$GIT_ROOT/.codalotl/cas`. This can be overriden with `$CODALOTL_CAS_DB`.
- Allowed to be written outside of sandbox dir.

## Checked into git

- These CAS files are intended to be checked into git. If `$CODALOTL_CAS_DB` is defined and outside the git repo (or git ignored), the behavior is undefined:
    - Storing CAS files outside the repo should work.
    - But some workflow items might break - for instance, the agent might use `git status` to notice new files.
