Check `SPEC.md` conformance for Go packages in the current module and record conforming packages in CAS.
- If `packages` is unset or empty, checked packages are those without a saved CAS entry asserting conformance, filtered by only_updated.
- If `packages` is present, only check these packages (even if already conforming), filtered by only_updated.