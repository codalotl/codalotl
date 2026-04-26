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
