## User Summary

Make package-mode `apply_patch` post-edit diagnostics and lints consistently check the selected Go package.

Automatic post-edit feedback is a core part of package mode. A successful patch should tell the agent whether its Go changes compile and should run the configured fix-mode lints for the package, even when one patch changes several files or includes supporting files such as fixtures and `testdata`.

The current implementation infers the check target from the parent directories of changed files. It silently skips all post-edit checks when a patch spans more than one directory. When a patch changes only a nested supporting file, it runs checks against that nested directory rather than the selected package. This makes check behavior depend on how files happen to be distributed within a package code unit and can omit expected formatting, linting, and diagnostic feedback.

Fix package-mode `apply_patch` so configured post-edit lints run against the selected package after every successful patch, regardless of how many included directories changed. Diagnostics should compile the selected package when the patch changes Go code. The tool result should continue to include the resulting diagnostic and lint output for the agent.
