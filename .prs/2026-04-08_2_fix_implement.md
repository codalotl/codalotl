# PR

## User Summary (do not modify)

I was trying /orchestrate. It decided it wanted a new package. Neat. So it made a SPEC.md file in a new folder. Great! It can called the implement tool on this "package". But it errored right away, because there were no .go files.

Just like you can enter /package mode in the TUI with a folder that has no .go files (or any files), implement should be callable on an existing folder with no .go files.

Testing:
- In addition to normal go test test cases, make sure you actually try your solution manually by using `go run . exec`, perhaps on some fixture data you create, in a tmp folder.

## Plan

### internal/agentbuilder

- Make package-mode default-context startup tolerate an existing target directory that does not yet load as a Go package.
- Keep rich initial context for normal Go packages, but fall back to package-path/env context instead of failing subagent startup when the directory has no `.go` files.
- Add coverage around the `package_mode_default_context` path so an `implement`-style subagent can target a SPEC-only or otherwise empty directory.

### Verification

- Run focused `go test` coverage for `internal/agentbuilder`.
- Manually verify with `go run . exec` against a temp fixture repo/package directory that contains `SPEC.md` but no `.go` files, and confirm `implement` can start instead of erroring immediately.

## Review

## Summary
