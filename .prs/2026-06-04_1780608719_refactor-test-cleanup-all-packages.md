# PR

## User Summary (do not modify)

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

