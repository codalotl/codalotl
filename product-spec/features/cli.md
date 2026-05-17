# CLI

Codalotl's executable is `codalotl`, and has many CLI-based commands.

Many of these commands will be documented in the product-spec of its feature.

## General

### <path/to/pkg>

When the CLI accepts a "package" argument:
- Go import paths are allowed and take precedence: "github.com/foo/bar/baz"
- CWD-relative dirs are allowed. Unambiguous versions start with ".". Ex: `.`, `./bar/baz`, `./..`.
- CWD-relative dirs that don't start with `.` are allowed, but are fallbacks. For instance, if there's a `fmt` dir, then just `fmt` refers to the stdlib, whereas `./fmt` refers to the local dir. But `foo/bar` refers to `./foo/bar`, provided `foo/bar` does not resolve to a package.
