## User Summary

Make `apply_patch` authorize the exact filesystem paths that it applies.

Authorization is a tool boundary that users and package mode rely on: every add, update, delete, and move must be checked before any part of the patch changes the filesystem. In package mode, this is also what keeps direct edits inside the selected package code unit.

The current implementation determines authorization paths separately from the patch behavior. Those two interpretations do not always agree. Accepted whitespace around operation headers can cause a target to be omitted from authorization, while leading whitespace in a path can cause one path to be authorized and a different path to be applied. As a result, an accepted patch can bypass its intended authorization check or the selected package boundary.

Fix `apply_patch` so every source and destination path it will actually mutate is authorized first, using the same path interpretation as patch application. If any required path is denied, the patch must not modify the filesystem. This must hold for all supported patch operations and accepted patch syntax, including moves and compatibility forms with surrounding whitespace.
