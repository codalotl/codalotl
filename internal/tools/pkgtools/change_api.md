change_api updates the public API (or public behavior) of an upstream Go package that the current package directly imports.
- Can change the public API of a package (ex: add methods, change params, alter fields on structs).
- Can change the public behavior of a package (ex: API signatures don't change, but a func behaves differnetly; fix bug you're observing).