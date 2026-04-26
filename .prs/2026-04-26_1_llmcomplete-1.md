# PR

## User Summary (do not modify)

Long-term, I want to delete the `llmcomplete` package in favor of `llmstream`.

In this PR:

Add this interface and constructor to `llmstream`:

```go
type Completer interface {
    Complete(ctx context.Context, modelID llmmodel.ModelID, systemMessage, userMessage string, options ...llmstream.SendOptions) (llmstream.Turn, error)
}

func NewCompleter() Completer
```

impl and test, obv.

This PR should only touch llmstream, i think.

## Plan

### Package `internal/llmstream` [DONE]
- Add `Completer` and `NewCompleter` to the public API.
- Implement `Completer.Complete` as a one-shot text completion helper over `NewConversation`, `AddUserTurn`, and `SendAsync`.
- Return the final assistant `Turn` from the successful completion event; return stream/preflight/provider errors as errors.
- Add focused tests for successful completion and error propagation.

## Review

Code review not run; user requested conformance only.

### SPEC conformance [DONE]

- `check_spec_conformance({"only_changed":true})` passed.
- Conforming changed packages reported: `internal/agentbuilder`, `internal/llmstream`.

## Summary

TODO

## Decisions

- Inside package `llmstream`, the interface method should use `SendOptions` and `Turn` directly. External callers see these as `llmstream.SendOptions` and `llmstream.Turn`.

## State

- Branch: `jn/llmcomplete-1`.
- PR file: `.prs/2026-04-26_1_llmcomplete-1.md`.
- Scope expected to stay in `internal/llmstream`.
- `internal/llmstream/SPEC.md` exists and controls public API.
- Implementation commit `7ff412e` added `internal/llmstream/completer.go` and `internal/llmstream/completer_test.go`.
- `go test ./internal/llmstream` passed after implementation.
- SPEC conformance passed for changed packages; CAS entries added under `.codalotl/cas/specconforms-1/`.
