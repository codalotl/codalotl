DRY up this Go package.

Look for opportunities to share helpers inside the package:

- Create missing helper functions when repeated logic is clear.
- Replace repeated logic with those helpers.
- Combine similar helper functions when one helper can express the shared behavior cleanly.
- Keep behavior unchanged.
- Keep changes package-local.
- Do not chase tiny similarities that would make code harder to read.

After editing, run focused package tests when appropriate and fix lint issues surfaced by the environment.
