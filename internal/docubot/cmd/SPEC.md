# docubot/cmd

This package is a minimal command runner for `docubot` for development/test purposes. It offers simple entry points into some of docubot's primary functions.

All options are via CLI flags (or environment variables, in the case of API keys) - no config file is read.

## Dependencies

- `internal/gocode` for Go package handling
- `internal/q/cli` for CLI structuring and command-line parsing

## Flags

All relevant commands accept:
- `--model` - which model to use
- `--reflow-width` - reflow width
- `--log-file` - path to a log file

## Commands

### Add documentation to a package:

```sh
go run . doc path/to/pkg
```

Additional options:
- `--test-files` - indicates that we document test files as well
- `--only-public-api` - only apply documentation for public/exported identifiers
- `--exclude-identifiers` - comma-separated list of identifiers to avoid documenting
- `--token-budget` - token limit to use
