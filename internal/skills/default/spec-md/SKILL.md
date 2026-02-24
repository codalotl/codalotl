---
name: spec-md
description: Guidance for creating, editing, and reviewing SPEC.md files in Go packages. Guidance for implementing/extending/modifying Go packages based on SPEC.md files.
---

# SPEC.md

This skill provides guidance for creating and editing SPEC.md files in Go packages.

A SPEC.md is the control panel of a Go package: it permits operators to manage facts about how the Go package behaves. Agents or engineers can detect diffs in SPEC.md files and create or modify Go packages that conform.

## Ambiguity

Unlike other "specifications", SPEC.md files are deliberately ambiguous, and should almost never be so detailed as to be unambiguous.

Again: ambiguity is GOOD and should be embraced. A natural temptation in reviewing SPEC.md files is to identify and remove ambiguity. Specify more things. But this is wrong. Ambiguity WILL be removed from the **implementation**, but the SPEC.md is not the finished product. It is simply a minimal document that can control and adjust behavior in a Go package.

A SPEC.md permits many valid conforming implementations. A good SPEC.md can quickly be evaluated with an implementation to output "conforming" or "non-conforming".

A good SPEC.md leaves unsaid design aspects where there's a good, obvious solution. If it's obvious to you that something should be specified, that might be a signal that it is in fact better left unsaid: it will probably be obvious to the implementor as well. A good SPEC.md is a **minimal** document that achieves a business outcome, where in the face of ambiguity, implementors will easily find good solutions.

### Trap ambiguity ("no good solution")

A "no good solution" situation is not mere ambiguity. It means that, given the current spec, any reasonable clarification still yields a bad result:

- Option A leads to Big Problem X.
- Option B leads to Big Problem Y.

In these cases, "clarifying the ambiguity" is not sufficient; the underlying approach should usually change.

## Consistent

A SPEC.md should be consistent with reality. For instance, it would be a mistake to base a design on a false belief about how Linux works.

A SPEC.md should also be internally consistent. That being said, it's neither possible nor desirable to add every exception or qualification to all sentences in the spec. Therefore, the spec must be evaluated in its totality: when the full spec is read, would a senior engineer find it contradictory?

This point often shows up when a general statement (which is often true) is in one part of the spec. But in a later, dedicated section, that general statement is broken down, each part qualified. Exceptions are made. The initial, imprecise general statement, combined with the dedicated section, must be evaluated together. The imprecise statement is NOT inconsistent.

## Implicit vs Explicit

Implicit requirements are first-class requirements. The refrain "make it explicit to reduce ambiguity" has no place here. (Explicit requirements are fine as well, but not everything needs to be explicitly spelled out.)

Example:
- In a `## Usage` section, a spec may refer to an identifier in this package. Even if the identifier is not in the `## Public API`, it is still a requirement that the package must expose this identifier.

## Terse

- Use concise, terse language. When creating SPEC.md files, often skip using articles like 'the' and 'an' (but don't remove them if the user writes them).
- Use bullet points to list details, often after a brief introductory non-bulleted paragraph.
- Delete non-essential words, sentence fragments, parentheticals, and full sentences, that don't add value.

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
- The implementation may be a superset (extra fields/methods/block elements are OK)

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

### Make implementation changes based on a SPEC.md change

When asked to "impl":
- If there's no existing implementation, just make an implementation that conforms to the spec, using good judgement in ambiguous situations.
- If there IS an existing implementation:
    - It may help to detect modifications to the spec with `git diff -- path/to/SPEC.md`, `git diff HEAD~1 HEAD -- path/to/SPEC.md`, or similar.
- Unless otherwise instructed, write tests.
    - Rule of thumb: 80% coverage at 20% of the cost.
    - When making small changes (e.g., based on a new section, or changes to existing sections), it sometimes does NOT warrant a NEW test case (these tend to violate the above rule of thumb).

### Check Conformance

When asked to "check conformance":
- Your goal is to decide whether the implementation of a package "conforms to" a SPEC.md file.
    - Do NOT fix any non-conformance. This is a read-only task.
- First, this requires the SPEC.md file to be consistent (see `## Consistent` above).
    - An implementation cannot conform to a contradictory SPEC.md.
    - Many "consistency" issues are actually non-issues: when the SPEC.md is read in its totality, the intent is clear. Don't fall into this trap.
- Run the fix lints tool - it runs `codalotl spec diff path/to/pkg`. It will identify mechanical differences between in the public API.
- Finally, determine if all the "facts" in the SPEC.md are matched by the implementation:
    - Create a list of facts in the SPEC.md. Go section-by-section, method-by-method, bullet-by-bullet.
    - Ensure the implementation abides by the fact.
- Output:
    - "The implementation conforms" or "The implementation does not conform".
    - A list of any non-conformances. Include whether it's a "major" or "minor", or "trivial" non-conformance.

(the following are common tasks; needs to be written)

### Synchronize SPEC.md and Implementation

### Create a SPEC.md from a high-level idea, user need, or high-level goal

### Create a SPEC.md based on an existing implementation
