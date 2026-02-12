---
name: spec-md
description: Guidance for creating, editing, and reviewing SPEC.md files in Go packages.
---

# SPEC.md

This skill provides guidance for creating and editing SPEC.md files in Go packages.

A SPEC.md is a specification of a Go package. It serves as both an initial recipe for creation of a Go package, and as documentation for the evolution and maintence of the Go package. Given a SPEC.md file, an engineer or agent can create a conforming Go package. Given both a SPEC.md and an implementation, an engineer or agent can answer the question: does the implementation conform to the spec?

## Ambiguity

SPEC.md files are deliberately ambiguous, and should almost never be so detailed as to be unambiguous (except in trivial cases). A SPEC.md file is not a detailed set of instructions given to a junior engineer, but rather, a guide for a senior engineer. The senior engineer still has latitude to make decisions. When the implementation is done, it can be evaluated against the SPEC.md file: does it match? A SPEC.md permits many valid matching implementations. A good SPEC.md can quickly be evaluated with an implementation to output "conforming" or "non-conforming".

One good candidate to leave ambiguous are design aspects where there's one or more good solutions. The implementor is free to just pick one.

### Trap ambiguity ("no good solution")

A "no good solution" situation is not mere ambiguity. It means that, given the current spec, any reasonable clarification still yields a bad result:

- Option A leads to Big Problem X.
- Option B leads to Big Problem Y.

In these cases, "clarifying the ambiguity" is not sufficient; the underlying approach should usually change.

## Consistency

A SPEC.md should be internally consistent. That being said, it's neither possible nor desirable to add every exception or qualification to all sentences in the spec. Therefore, the spec must be evaluated in its totality: when the full spec is read, would a senior engineer find it contradictory?

This point often shows up when a general statement (which is often true) shows up in one part of the spec. But in a later, dedicated section, that general statement is broken down, each part qualified. Exceptions are made. The initial, imprecise general statement, combined with the dedicated section, must be evaluated together. The imprecise statement is NOT inconsistent.

## Terse

- Typically use concise, terse language. When creating SPEC.md files, often skip using articles like 'the' and 'an' (but don't remove them if the user writes them).
- Use bullet points to list details, often after a brief introductory non-bulleted paragraph.

## Typical Sections

The SPEC.md should have a top-level `#` section with the name of the package, and a brief overview. This section may discuss motivations and use cases.

The most valuable section is `## Public API` (as explained below). Other sections that can be added as needed:
- `## Usage` or `## Example Usage`: might give some ```go``` fenced blocks of package usage.
- `## Dependencies`: list package dependencies
- `## Supported Platforms` (only when relevant)

Beyond that, `##` sections can be added (with `###` underneath, etc) in order to describe domain topics of the package. For example, a terminal TUI library might have sections like `## Signals`, `## Keyboard Input`, `## Events`, `## Copy/Paste Support`, and so on.

### Public API

The SPEC.md should usually have a `## Public API` section, which consists of one or more ```go``` fenced blocks.
- This documented Public API is usually not exhaustive/complete.
- The Go identifiers in Public API should usually have documentation.
- Any doc comments should be matched verbatim in the implementation. These public APIs (including doc comments) should be kept in sync.

## Common Tasks

In all of the following tasks, follow the user's instructions, as their request might not align perfectly to one of these.

### Review a SPEC.md

When asked to "review":
- Avoid commenting on the implementation. It may be missing, incomplete, out of date, etc. The user wants you to focus on the spec.
    - You can look at the implementation and other supporting files/packages, but don't give feedback that the implementation is missing, out of date, non-conforming, etc.
    - However, you can assume that dependency packages are fully baked: if the design relies on a dep, and the dep doesn't support how the SPEC.md wants to use it, that's worth flagging.
- Fix obvious typos and misspellings (use apply_patch; don't just report it).
- Fix grammar, but keep in mind we want terse specs, so missing articles are fine (use apply_patch; don't just report it).
    - Also fix documentation typos/grammar in Public API (even if it introduces divergence with the implementation).
- Do these, but don't autofix unless otherwise instructed to (instead, just tell the user):
    - Check for internal consistency (but very important: not all exceptions/aspects of a design should be repeated in all sentences/sections).
    - Check for ambiguous design aspects that are worth flagging or specifying:
        - The documentation in the Public API is a good place to reduce ambiguity - it's not just SPEC.md ambiguity, it's interface ambiguity. Suggesting high-leverage clarifications here is fine.
        - Trap ambiguity ("no good solution"): any reasonable clarification leads to a materially bad outcome. In these cases, the underlying design likely needs to change; merely "clarifying" won't fix it.
        - Beyond these two, the bar to point out ambiguous design choices is incredibly high. Remember, ambiguity at the SPEC.md level is GOOD (it resolves during implementation). Only point out CRITICAL aspects that should be clarified.

When asked to review a new section of the SPEC.md, you may use `git diff -- path/to/SPEC.md` to see changes.


(the following are common tasks; needs to be written)

### Check Conformance

### Synchronize SPEC.md and Implementation

### Create a SPEC.md from a high-level idea, user need, or high-level goal

### Create a SPEC.md based on an existing implementation
