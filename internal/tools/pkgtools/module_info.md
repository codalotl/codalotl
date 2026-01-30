module_info returns information about the current Go module.
- Includes go.mod contents (module path, Go version, require/replace blocks, etc.).
- Lists packages in this module, optionally filtered by a Go RE2 regexp.
- Optionally includes packages from direct dependency modules (as listed in go.mod, excluding `// indirect`).

