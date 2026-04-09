
# PR

## User Summary (do not modify)

See agentregistry and agentbuilder. Subagents currently have a configuration for skills in the .yml file (true or false, default to true). I want the same for agentsmd (default true). When true, the AGENTS.md file is included as a message. This supplants the current impl, which only adds it to root agent sessions in tui/noninteractive.

This will result in AGENTS.md being included in our other agents (change_api, etc). If integration tests fail, read the SPEC.md. Manually patch the corresponding http.json file(s).
