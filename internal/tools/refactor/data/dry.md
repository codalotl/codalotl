DRY up this Go package.

Look for opportunities to share helpers inside the package:

- Create missing helper functions/vars/consts/types when repeated logic is clear. Replace repeated logic with those helpers.
- Combine similar helper functions when one helper can express the shared behavior cleanly.
- Keep behavior unchanged.
    - EVEN if you notice a bug, don't fix it. Behavior must be identical.
    - Report any possible bugs you happen to notice in your final report (but don't go hunting for bugs specifically).
- You MUST NOT change the public api of the package: don't change documentation on the exported api, don't add/remove parameters to exported functions, don't add/remove exported identifiers.
- Keep changes package-local.
- Do not chase tiny similarities that would make code harder to read.

Important: do not make an edit for marginal improvements. The code will attempted to be iteratively DRY'ed up. This is the 3rd iteration. It is completely appropriate to state that DRYing opportunities are worthwhile.

After editing, run package tests when appropriate and fix lint issues surfaced by the environment.

If you produced a changeset, self-assess based these dimensions. Use a 1-5 scale (1: least, 5: most).
- Does this changeset DRY up code? (1: barely; 5: a lot)
- Does this changeset make code reading and understanding easier? (1: way harder; 3: unchanged; 5: way easier)
- Overall, is this changeset worth merging?

In your final message:
- Briefly summarize what you did.
- Report any bugs you happened to notice.
- Report your self-assessments.
