# docubot/cmd

This package is a minimal `main` runner for `docubot` for development/test purposes. It offers simple entrypoints into some of docubot's primary functions.

All options are via CLI flags or ENV - no config file is read.

## Dependencies

- `internal/gocode` for Go package handling
- `internal/q/cli` for CLI structuring and command-line parsing

## Flags

All relevant commands accept:
- `--model` - which model to use
- `--reflow-width` - reflow width
- `--log-file` - path to a log file

## Commands

### Add documention to a package:

```sh
go run . doc path/to/pkg
```

Additional Options:
- `--test-files` - indicates that we document test file as well
- `--exclude-identifiers` - comma separated list of identifiers to avoid documenting
- `--token-budget` - token limit to use