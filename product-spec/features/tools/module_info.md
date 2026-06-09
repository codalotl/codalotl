# `module_info`

`module_info` inspects Go module metadata and package lists.

## Inputs

- `package_search`: optional Go RE2 regular expression used to filter returned packages.
- `include_dependency_packages`: optional boolean; when true, the package list also includes packages from direct dependency modules.

## Output

The tool returns Go module information followed by a package list. When package filtering or dependency-package inclusion is requested, the result makes that scope visible.

The result may also include compact package-list context when available, so the agent can distinguish packages in the current module from packages found in dependency modules.

Errors include invalid parameters, invalid package search expressions, missing or unusable Go module context, denied permissions, and failures while reading module or package information.

## Behavior

- The tool discovers the Go module from the package-mode sandbox context.
- The tool returns information from `go.mod`, including module path, Go version, requirements, replacements, and similar module declarations.
- The tool lists packages defined in the current module.
- The agent can filter the package list with a Go RE2 regular expression.
- The agent can ask to include packages from direct dependency modules listed in `go.mod`.
- Dependency-package listing excludes indirect-only dependencies.
- The tool is module-scoped, so it can help the agent find related packages before using narrower Go-aware tools.

## Presentation

Example display:

```text
• Read Module Info
```

When the agent supplies options, the presentation shows a compact option summary:

```text
• Read Module Info
  └ Search: ^github.com/example/project/internal/foo$; Deps: true
```

The presentation should not dump the full module information or package list into the progress line.

## Permissions

Module information is authorized before it is read.

In package mode, `module_info` intentionally provides module-level discovery even though ordinary file access is scoped to the selected package code unit. Including dependency packages may require broader read authorization because it inspects package information outside the current module.
