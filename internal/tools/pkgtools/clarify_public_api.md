clarify_public_api requests clarification about a Go package's documentation for a specific identifier.
- The `path` parameter may be either a package directory within the sandbox (relative to the sandbox root) or a Go import path.
    - The Go import path may refer to packages inside the sandbox, or dependency packages outside the sandbox.
- Provide the target `identifier`. Examples: `MyVar`; `MyConst`; `SomeFunction`; `ImportantType`; `*SomeType.FooMethod`; `SomeType.BarMethod`.
- Make sure you've read the existing docs for the identifier.
