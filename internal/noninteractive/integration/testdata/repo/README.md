# Integration Fixture Repo

This module is intentionally a little richer than a toy project. It models a tiny order-planning system with enough package boundaries to exercise:

- upstream API changes via `change_api`
- public-behavior questions via `clarify_public_api`
- downstream propagation via `update_usage`

Package graph:

- `catalog` provides product metadata and tagging helpers
- `inventory` depends on `catalog`
- `pricing` depends on `catalog`
- root package `orders` depends on `catalog`, `inventory`, and `pricing`
- `reporting` depends on `orders` and `pricing`

The shared text fixtures used by existing integration tests are still present:

- `hello.txt`
- `docs/note.txt`
