get_usage retrieves cross-package usage details for a Go identifier.
- The `defining_package_path` parameter may be either a package directory within the sandbox (relative to the sandbox root) or a Go import path.
    - Typically the package you're currently assigned to.
- Provide the target `identifier` (defined in `defining_package_path`). Examples: `MyVar`; `MyConst`; `SomeFunction`; `ImportantType`; `*SomeType.FooMethod`; `SomeType.BarMethod`.
