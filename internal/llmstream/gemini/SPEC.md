# gemini

The gemini package implements a minimal client to perform streaming requests to the Gemini LLM.

## Dependencies

No third party "Google SDK"-style modules are used. This package should not make net-new deps that the rest of the repo does not need.

- `internal/q/sseclient` for SSE.

NOTE: Currently, the `google.golang.org/genai` is in this module, as well as all its dependent modules. NONE of those may be used here. This package is being written to replace those.

## Scope

Only the portion of the API needed to implement `llmstream` will be implemented:
- Only `streamGenerateContent`

## Testing

Employs both stubbed tests (don't hit actual endpoints) and integration tests (hit gemini endpoints).

Integration tests are gated behind the `INTEGRATION_TEST` env var. When this is set (to any non-empty value), it reads and uses `GEMINI_API_KEY`.
