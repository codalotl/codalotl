change_api updates the public API (or public behavior) of an upstream Go package that the current package directly imports.
- Can change the public API of a package (ex: add methods, change params, alter fields on structs).
- Can change the public behavior of a package (ex: API signatures don't change, but a func behaves differnetly; fix bug you're observing).
- The `path` parameter may be either a package directory within the sandbox (relative to the sandbox root) or a Go import path.
    - Must resolve to an upstream package that the current package directly imports.
    - Will only modify packages within the sandbox; it will return an error for standard library packages or dependency packages in the module cache.