# PR

## User Summary (do not modify)

grant codalotl_cli access to `codalotl spec status`

Ask the user to restart the TUI. Then use that to detect and verify the spec conformance of all packages that lack conformance (ignore ones without SPEC.md).

For any nonconformance, assess. Either update SPEC.md to match behavior, or fix code to match match spec. Optimize for UX and use good judgement.
