# Codalotl

## Getting Started

[[installation is go install github.com/codalotl/codalotl@latest]]

## Specific Go Support

[[what makes codalotl different than other agents; how it supports Go specifically; look at README.md when generating this text]]

## Package Mode

## TUI

### Slash Commands

### Permissions

### Keyboard Input

[[control keys]]

### Details View

[[shows mode, token usage]]

### Overlay Mode

#### Copying Text

[[not great currently; can be done with overlay mode]]

## CLI

[[add a nested header for each command]]

## Configuration

### Config File

### Models

### AGENTS.md

### Skills

### Lints

[[
    - gofmt is only default
    - reflow is highly recommended; set reflow max width in config; recommend a 1-time reflow of repo, to avoid unrelated diffs on miscellaneous tasks/commits
    - can add staticcheck and golangci-lint with extend + id
]]

## Safety & Security

[[
    - sandbox dir; shell allowed commands; note that package mode does not use shell commands at all (except for skills)
    - note that there is no OS-level sandboxing; users wanting true security can run in a docker container or similar
    - note that UX is prioritized above true security (as long as true security can be achieved with containerization)
]]

## Status & Limitations

### Supported Platforms

### Unsupported Features

[[mcp will likely never be supported; use shell commands and skills]]
