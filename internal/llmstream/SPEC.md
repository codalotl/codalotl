# llmstream

llmstream is an abstraction over LLM providers, offering a unified interface. Streaming only.

## Providers

### OpenAI

- Implements responses API only.

### Anthropic

- Only supports Opus/Sonnet 4.6+.
- Hard-codes 32k max_tokens
- Uses "adaptive" thinking type (budget omitted).
- `Options.ReasoningEffort` maps appropriately to `output_config { effort }`.

## Public API