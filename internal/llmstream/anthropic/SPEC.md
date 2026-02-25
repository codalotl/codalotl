# anthropic

The anthropic package implements a minimal client to perform streaming requests to the Anthropic LLM.

## Dependencies

No third party "Anthropic SDK"-style modules are used. This package should not make net-new deps that the rest of the repo does not need.

- `internal/q/sseclient` for SSE.

## Scope

Only the portion of the API needed to implement `llmstream` will be implemented:
- `/v1/messages`
- Only streaming

Not:
- `/v1/messages/batches`, `/v1/messages/count_tokens`, `/v1/models`, `/v1/skills`, `/v1/files` (this list is not exhaustive)

## Testing

Employs both stubbed tests (don't hit actual endpoints) and integration tests (hit anthropic endpoints).

Integration tests are gated behind the `INTEGRATION_TEST` env var. When this is set (to any non-empty value), it reads and uses `ANTHROPIC_API_KEY`.

## Docs

API documentation from Athropic can be found on disk (saved 2026-02-25):
- https://platform.claude.com/docs/en/api/overview - `docs/api_overview.md`
- https://platform.claude.com/docs/en/api/messages - `docs/create_message.md`
