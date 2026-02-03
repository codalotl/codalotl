Fetch public-facing API and documentation for a Go package.
- Returns concise godoc-style documentation for exported identifiers only.
- The `path` parameter may be either a package directory within the sandbox (relative to the sandbox root) or a Go import path.
- Works for packages in the current module, in the module dependency graph, and in the Go standard library.
- Use this tool if you want to **use** the specified package from your current package.
- Optionally, supply specific identifiers to fetch docs for.
