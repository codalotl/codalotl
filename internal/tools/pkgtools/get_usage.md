get_usage retrieves cross-package usage details for a Go identifier.
- Provide the `defining_package` - the import path of package you're currently assigned to.
- Provide the target `identifier` (defined in `defining_package`). Examples: `MyVar`; `MyConst`; `SomeFunction`; `ImportantType`; `*SomeType.FooMethod`; `SomeType.BarMethod`.
