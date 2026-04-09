
# PR

## User Summary (do not modify)

See agentregistry and agentbuilder. Subagents currently have a configuration for skills in the .yml file (true or false, default to true). I want the same for agentsmd (default true). When true, the AGENTS.md file is included as a message. This supplants the current impl, which only adds it to root agent sessions in tui/noninteractive.

This will result in AGENTS.md being included in our other agents (change_api, etc). If integration tests fail, read the SPEC.md. Manually patch the corresponding http.json file(s).

## Plan

### [DONE] `internal/agentbuilder`

- Extend YAML agent config with optional `agentsmd` boolean, defaulting to true like `skills`.
- Move AGENTS.md injection into registry-built agent definitions so YAML-defined root agents and subagents get the same behavior.
- Keep generic and package-mode lookup behavior aligned with existing callers:
  - generic agents read AGENTS.md from the sandbox root
  - package agents read AGENTS.md from the target package dir
  - package default-context agents keep AGENTS.md ahead of generated package context without duplicating it
- Update `SPEC.md` if the YAML config surface or prompt/initial-turn behavior needs documenting.

### [DONE] `internal/tui` and `internal/noninteractive`

- Remove session-layer AGENTS.md injection that becomes redundant once agent definitions provide it.
- Preserve existing environment info and package initial context ordering while avoiding duplicate AGENTS.md turns.

### [DONE] Tests and fixtures

- Add focused `agentbuilder` coverage for:
  - default `agentsmd: true`
  - explicit `agentsmd: false`
  - generic vs package-mode initial turns
  - coexistence with `include_package_mode_context`
- Run the relevant package tests and patch any affected integration `http.json` fixtures if prompt/turn snapshots change.
  - Verified with `go test ./...`.
  - No integration fixture patches were needed.

## Review

- Actionable review feedback:
  - [P2] `internal/agentbuilder`: keep AGENTS.md read failures best-effort.
    - Current implementation routes AGENTS.md reads through agent-definition initial-turn builders.
    - If `agentsmd.Read(...)` fails (for example unreadable `AGENTS.md`), `registry.Prepare` now fails for built-in generic/package agents and YAML-defined subagents.
    - Previous TUI/noninteractive behavior treated AGENTS.md reads as best-effort and continued startup without that context.
    - Follow-up needed: preserve best-effort AGENTS.md behavior while still injecting AGENTS.md turns through the registry path.

## Summary
