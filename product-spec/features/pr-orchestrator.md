# PR Orchestrator

The PR Orchestrator is an agent and workflow that takes implements a PR (pull request) from user summary to reviewed, final product. It does not deal with actual pull requests in Github - instead, it builds the commits for one in a local branch.

Typical Workflow (example):
- User types `codalotl pr new my-feature`
    - This makes a local branch off of main, adds a PR File like `.prs/2026-05-19_1779211919_cas-prune.md`
- User starts by typing their feature description and requirements in the PR File.
- Starts TUI. Types `/orchestrate`. The orchestrator agents start working according to its workflow.
- The orchestrator does one step of workflow at a time, using the PR file to keep track of plans, progress, and state. User can use new sessions, or keep telling the orchestrator to continue (by literally typing something like "continue" and sending as a message to the agent).
- Eventually the orchestrator will be done. Various commits in the branch will be made.
- User can then manually push, make a real PR, or do whatever they want.

## Orchestrator Prompt

The orchestrator prompt lives at `internal/agentbuilder/data/pr-orchestrator.prompt.md`. It describes many of the facts and concepts of the orchestrator. In order to avoid duplication, it serves as a co-spec to this document. They should not contradict, and should be small. Provided it doesn't contradict, consider it a source of truth for how the PR Orchestrator works.

## PR File

PR files live in `.prs`. They have filenames like `YYYY-MM-DD_<unix-seconds>_<feature-name>.md`.

Initial template:

```markdown
# PR

## User Summary (do not modify)


```

(It just has 2 sections, with a couple blank lines for the user to easy type their text).

## CLI

### codalotl pr new <feature-name> [--no-git]

This makes a new PR File with proper naming and sets up a git branch for the orchestrator to work on it:
- Make sure on main/master, up to date, and clean workspace.
- Make new branch in the format `$CODALOTL_USER_INITIALS/feature-name`. For instance, if my initials are `jn`, a branch might be `jn/add-orchestrator-pr-new` If `$CODALOTL_USER_INITIALS` is unset, just `add-orchestrator-pr-new`.
- Make new PR File at `.prs/YYYY-MM-DD_<unix-seconds>_<feature-name>.md`, templated with the initial template above.
- Adds the file, and commits it.
- If origin is set up, pushes to origin with remote tracking.
- If `--no-git`, then none of the git stuff is done. We just make the file.

### codalotl pr refactor (--package=<path/to/pkg> | --all-packages) [--refactor=<name>]

This is a special case of `codalotl pr new`.
- One package selector is required:
    - `--package=<path/to/pkg>` targets one package.
    - `--all-packages` targets all Go packages in the current module.
- `--refactor=<name>` is optional. Without it, run all refactors we have.
- Feature name is automatically generated. Examples:
    - `refactor-internal-mypkg` for `--package=internal/mypkg`
    - `refactor-docs-fix-all-packages` for `--all-packages --refactor=docs-fix`
- PR file pre-baked instructions include what to run, how to commit/review, how to skip no-op or risky changes, and how to deal with CAS files and recertify them.
- `--package=<path/to/pkg>` with no `--refactor` runs all refactors on one package.
- `--all-packages --refactor=<name>` runs one refactor across all packages. Due to the CAS system, this will only refactor packages that are not up-to-date in their refactors.
- `--all-packages` without `--refactor` is not supported. It makes PRs too large.
- The user can then run the orchestrator as normal, possibly customizing the PR file as they see fit beforehand.

### codalotl pr prune [--days=N]

This deletes PR files older than N days (default: 30). It does not commit anything. Time is based on mtime of file.
