# Product Spec Conformance

This product spec gives an overview of the product, and contains details at various levels of granularity. The product should conform to this spec.

This document defines how product specs work - their norms, meanings, conventions, and how to interpret them - and how to check whether the product conforms to the spec.

## Basics

- The product spec is meant to be high level and ambiguous. It's not meant to specify every last detail.
- It's meant to be user focused. Describe things based on how users experience them, not based on implementation.
- Capability-based requirements work well: "the user can do X in order to accomplish Y".

## Complexity & Precision of Language

Actual software has edge cases, caveats, and nuances. We do not attempt to specify software in this product spec that fully captures all of that. We keep it higher-level, and capability based.

This means that it's exceptionally difficult to state requirements that are both high level AND 100% mathematically true. For instance, "when the user issues X command, they log into OpenAI." However, that might not always be true: what happens if they're already logged in? What happens if we cannot establish a connection? What happens if they overrode the OpenAI auth URL? What happens if OpenAI returns error Y?

We still allow and want high-level statements like "when the user issues X command, they log into OpenAI". But it must be interpreted with human common sense. Edge cases are going to be present in nearly all requirements, and they don't need to be specified. When this happens, it doesn't mean the product doesn't conform to the product spec.

### Language used to indicate imprecision

- `like` indicates an example. Ex: "X adds a PR File like `.prs/2026-05-19_1779211919_cas-prune.md`"
- Tilde (`~`) indicates "roughly" or "nearly" or "approximately". Ex: "When Y, ~all of the LLMs should Z". This might mean 1 or 2 LLMs might be exceptions.
- `might` indicates one possibility or example. The possibility given should at least be possible with the product.
- (This list is not exhaustive. Similar language can also be used.)

NOTE: the lack of imprecise language does NOT mean a statement in the spec should necessarily be interpreted as a fully precise statement with no nuance. One must still use judgement.

## Superset

The actual product is a superset of the spec in two important ways.

First, and most obvious: the product may have more capabilities than are mentioned in the spec.

Second, when the spec describes a feature, the actual feature may be a superset of what is specified:
- It may implement unspecified edge cases and error handling.
- It must do *at least* what is specified, but it may also do *more*.
- It may "essentially" do what is specified, but be fundamentally more complex.

### Superset Example

For example, a spec might say, "The user can enter their email into the Email field, press Submit, which sends a password reset to the inbox". The actual product might:
- Validate the email is valid and display an error to the user.
- Rate limit resets in various ways.
- Allow the input of a username instead of an email, and send to the corresponding email.
    - This allowed implementation may be surprising to you, but is very important. It's an example of doing "essentially" what is specified, but is fundamentally more complex. In other words, it is absolutely true that the user can enter their email. But also, the user can enter other things as well. The product "covers" what is specified, but goes further. This is fine.

## Conformance

A product **conforms** to the spec if the product covers all use cases and requirements mentioned in the spec.

Keep in mind:
- The product may be a superset.
- Not all edge cases need to be mentioned.
- Use human judgement. Would a human think that the product conforms?
    - Don't be a pedantic nit.

### Checking Conformance

- Consider each product spec document separately. For instance, `feature_a.md`.
- Given the document above, consider it in the **context** of the rest of the documents. Some requirements are "factored" across multiple documents.
    - Read the tree of the `product-spec` directory (or whatever dir contains the overall spec).
    - Read what looks like important and related files.
    - Read any "governing" documents in higher-level directories. For example, read documents like `OVERVIEW.md` and `PHILOSOPHY.md` and similar.
    - Read any documents that might have pieces of the spec factored into them. For example, given `product-spec/features/foo/a.md`, read files like `product-spec/features/foo/foo.md` or `product-spec/features/foo/common.md`.
    - Generally, it's more beneficial than not to get a complete picture from the product spec.
- Form an internal list of features and requirements that the spec describes.
- Validate each one according to the norms described above.
- If all requirements are met, the product conforms to the spec.
- Report to the user any non-conformance. Categorize each as trivial, minor, or major.
