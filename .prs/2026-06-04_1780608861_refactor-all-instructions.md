# PR

## User Summary (do not modify)

When I run `go run . pr refactor --all-packages --refactor=test-cleanup`, I get these instructions in the PR file:

```text
In this PR, run the test-cleanup refactor across all Go packages in the current module.

Target: all Go packages in the current module
Selected refactor flow: test-cleanup

For each package in the current module:
1. refactor("name": "test-cleanup", "package": "<package>")

Additional instructions:
- Inspect each refactor result and diff before moving to the next package.
- Commit accepted changes with source changes and relevant CAS files. Prefer focused commits per package or small package group.
- Skip no-op packages without a commit and add a note in this PR file.
- If a package looks risky or outside scope, do not fix-forward aggressively; revert/skip it and add a note in this PR file explaining why.
- Due to CAS, packages already up to date for this refactor may be no-ops.
- After final accepted changes, use the codalotl_cli tool for each accepted package that needs recertification:
  codalotl cas recertify <package> --namespaces="refactor-test-cleanup"
- Inspect and commit CAS files produced by recertify.
```

Change this to say to run that refactor across all needed Go packages (current module is incorrect - there could be multiple modules).

Further, explain how to get a list of needed packages: Use `codalotl_cli` to run `codalotl cas ls-summary refactor-test-cleanup`. This gives you a list of packages that need to be fixed. (cas ls-stale might need to be added )