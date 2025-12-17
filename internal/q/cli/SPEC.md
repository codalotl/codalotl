# cli

cli is our standard library for making CLI applications:
- command/arg/flag parsing
- Automatic help/usage generation.

## Concepts

We support making CLI apps in the git/go style. Examples:
- `git checkout mybranch`
- `go test . -v -run=TestThing`

The pattern is APPNAME COMMAND ARG --FLAG.

## Not In Scope

This may change later, but for now, these are not in scope:
- completions
- man pages
