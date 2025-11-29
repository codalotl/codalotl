clarify_public_api requests clarification about a Go package's documentation for a specific identifier.
- Provide the target `path` (dir to package, or file) that contains the identifier's documentation. `path` is absolute or relative to the sandbox.
- Provide the target `identifier`. Examples: `MyVar`; `MyConst`; `SomeFunction`; `ImportantType`; `*SomeType.FooMethod`; `SomeType.BarMethod`.
- Make sure you've read the existing docs for the identifier.
