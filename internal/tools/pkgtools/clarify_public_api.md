clarify_public_api requests clarification about a Go package's documentation for a specific identifier.
- Provide the target `path`, which is either:
  - a Go package directory (relative to the sandbox), or
  - a Go import path (may resolve to a dependency package outside of the sandbox).
- Provide the target `identifier`. Examples: `MyVar`; `MyConst`; `SomeFunction`; `ImportantType`; `*SomeType.FooMethod`; `SomeType.BarMethod`.
- Make sure you've read the existing docs for the identifier.
