# PR

## User Summary (do not modify)

I was trying /orchestrate. It decided it wanted a new package. Neat. So it made a SPEC.md file in a new folder. Great! It can called the implement tool on this "package". But it errored right away, because there were no .go files.

Just like you can enter /package mode in the TUI with a folder that has no .go files (or any files), implement should be callable on an existing folder with no .go files.

Testing:
- In addition to normal go test test cases, make sure you actually try your solution manually by using `go run . exec`, perhaps on some fixture data you create, in a tmp folder.

## Plan

### [DONE] internal/agentbuilder

- Make package-mode default-context startup tolerate an existing target directory that does not yet load as a Go package.
- Keep rich initial context for normal Go packages, but fall back to package-path/env context instead of failing subagent startup when the directory has no `.go` files.
- Add coverage around the `package_mode_default_context` path so an `implement`-style subagent can target a SPEC-only or otherwise empty directory.

### [DONE] Verification

- Run focused `go test` coverage for `internal/agentbuilder`.
- Run `go test ./...`.
- Manually verify with `go run . exec` against a temp fixture repo/package directory that contains `SPEC.md` but no `.go` files, and confirm `implement` can start instead of erroring immediately.

## Review

- Review status: [DONE] non-actioned.
- Reviewed concern:
  - Restrict the new fallback package-context path to empty/SPEC-only directories and keep surfacing raw `buildGoPackageInitialContext` failures for real Go packages.
- Resolution:
  - Not actioned. From the product/user perspective, `implement` should still start for an existing target package directory even when the package has broken Go files and the rich package initial context cannot be built.
  - The fallback context is intentionally broader than the empty-directory case: it keeps package-mode startup alive and includes the underlying load/init error text in the fallback context, instead of failing immediately.

## Decisions

- `implement` should tolerate package-context startup failures for an existing directory inside a Go module, including syntax/build-broken package states, by falling back to package-path context instead of aborting.

## Summary

Allow `implement` package-mode startup to target an existing directory that does not yet load as a Go package.

- Update `internal/agentbuilder` package-mode default context to fall back to package-path context when rich Go package initial context cannot be built.
- Preserve useful startup context in the fallback path by including module/package path metadata, a directory listing, and explicit unknown-status blocks instead of failing immediately.
- Cover the new behavior with focused tests for both direct package-mode initial-turn building and the `implement` tool prepare path.
- Verify with focused `go test`, full `go test ./...`, and manual `go run . exec` against a temp SPEC-only package directory.
