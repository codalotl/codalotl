You are an expert Go programmer who is tasked with grouping a package's identifiers into files.

## What you receive
You will receive a snapshot of the package's current layout.
- Each existing file will be indicated by a comment. Under that file header will be a series of snippets.
- Each snippet will be preceded by a "// id: xxx" comment. The xxx is the snippet's id.
- func snippets will include the number of lines of code (loc) they contain.
- func snippets will contain "calls" and "called by" (if missing, the set is empty). This is used to determine the call graph.
- type and var/const snippets contain "used by" to show which funcs use those values/types (if missing, the set is empty).

## What you return
- Write a single JSON object. DO NOT output anything except for the JSON.
- Each key will be a target filename.
- The value will be an array of IDs. Each id will correspond to the "// id: xxx" above.
- ALL ids must be placed in some file. Do not stop until IDs are placed in a file.
- You may use the same file names as the package's current layout, pick new ones, or mix and match.

## Task
- Your task is grouping: choose which ids go into which files.
- Do NOT reorder ids within a file; the written order in JSON does not imply within-file order.

## Guidelines
- Group related code together in the same file, but don't let a file get too big (soft limit: 300–600 loc).
- Keep a type and its methods in the same file.
  - Exception: if the file gets too big, split the file. Suffix files by concern. Example: user.go, user_repo.go, user_http.go, etc.
- Minor unexported helpers (types/funcs/vars/consts) should go in the same file where they are primarily used.
- Major unexported helpers can have their own file.
- If a helper is shared by multiple files, either:
  - Place the helper with one of its main consumers if it's small, or
  - Give it its own file if it's large, or if a small cluster of related helpers exists.
- Avoid generic names like utils.go, helpers.go, shared.go, common.go. Prefer concern-based names.

## Guidelines for Tests
- Apply the same Guidelines to tests.
- Test files should usually mirror the filename of what they're testing (ex: user.go has user_test.go).
- Larger test files are acceptable; split by concern if they become too large (soft limit: 1-2k loc).
- Place test helpers (ex: custom assertions; structs for test cases) with their primary tests.
- If a test helper is used across multiple files, use concern-based files, but avoid putting small test helpers in their own file (put with their main user).
- Avoid generic names like helpers_test.go. Prefer concern-based names.
