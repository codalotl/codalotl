# Product Spec Guidance

This product spec gives an overview of the product, and contains details at various levels of granularity. The product should conform to this spec.

This document gives guidance on the spec itself - rules, norms, and conventions.

## SPEC.md

Read the $spec-md skill: it provides many of the foundational elements that govern this spec. Whereas the SPEC.md specifies a Go package, this spec is for the overall system/product.

## Basics

- The product spec is meant to be high level and ambiguous. It's not meant to specify every last detail.
- It's meant to be user focused. Describe things based on how users experience them, not based on implementation.
- Capability-based requirements work well: "the user can do X in order to accomplish Y".
- Ultimately, we are building a conformance test: does the actual product conform to the product spec?

## Complexity & Precision of Language

Actual software has edge cases, caveats, and nuances. We do not attempt to specify software in this product spec that fully captures all of that. We keep it higher-level, and capability based.

This means that it's exceptionally difficult to state requirements that are both high level AND 100% mathematically true. For instance, "when the user issues X command, they log into OpenAI." However, that might not always be true: what happens they're already logged in? What happens if we cannot establish a connection? What happens if they overrode the OpenAI auth URL? What happens if OpenAI returns error Y?

We still allow and want high-level statements like "when the user issues X command, they log into OpenAI". But it must be interpreted with human common sense. Edge cases are going to be present in nearly all requirements, and they don't need to be specified. When this happens, it doesn't mean the product doesn't conform to the product spec.

### Language used to indicate imprecision

- `like` indicates an example. Ex: "X adds a PR File like `.prs/2026-05-19_1779211919_cas-prune.md`"
- Tilde (`~`) indicates "roughly" or "nearly" or "approximately". Ex: "When Y, ~all of the LLMs should Z". This might mean 1 or 2 LLMs might be exceptions.
- `might` indicates one possibility or example. The possibility given should at least be possible with the product.
- (This list is not exhaustive. Similar language can also be used.)

NOTE: the lack of imprecision language does NOT mean a statement in the spec should necessarily be interpreted as a fully precise statement with no nuance. One must still use judgement.

## Orthogonality

Each document's information should be ~80% orthogonal to other documents' information. The product spec is meant to be understood in totality, and should be mostly factored so that we don't repeat ourselves. That being said, it's helpful to refer to other product features and concerns and have some overlap when it helps a document read easier.

## Future

Some ideas may be added to the product spec that are not intended to be implemented yet. These are indicated with the word "future" in some way. For instance, either a section called `## Future`, or a bullet like `- FUTURE: do xyz`.
