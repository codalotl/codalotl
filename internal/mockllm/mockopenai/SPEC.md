# mockopenai

The `mockopenai` package implements a mock HTTP server for a subset of the OpenAI API, for testing.

Input and output are provided via a JSON file. The response is streamed using SSE.

## Example Usage

Given a JSON or JSON-with-comments file:

```jsonc
{
    "responses": [
        {
            // name is optional metadata only
            "name": "initial request",

            // if true, once this request is used, it cannot be matched again.
            "consume": false,

            // Request fields to /v1/responses
            "request": {
                "model": "gpt-5.4",
                "input": "Tell me a three sentence bedtime story about a unicorn."
            },

            // headers: optional request headers
            "headers": [
                { "name": "X-Tenant-ID", "value": "tenant-a" },
                { "name": "Authorization", "value": {"match": "partial", "text": "Bearer"} }
            ],

            "response": {
                "id": "resp_67ccd2bed1ec8190b14f964abc0542670bb6a6b452d3795b",
                "object": "response"
                // ...
            }
        },

        // ... more responses ...
    ]
}
```

Go usage:

```go
handler, err := mockopenai.NewHandlerFromFile("testdata/openai-responses.jsonc")
if err != nil {
    return err
}

srv := httptest.NewServer(handler)
defer srv.Close()

reqBody := `{"model":"gpt-5.4","input":"Tell me a three sentence bedtime story about a unicorn."}`
req, err := http.NewRequest(http.MethodPost, srv.URL+"/v1/responses", strings.NewReader(reqBody))
if err != nil {
    return err
}

resp, err := http.DefaultClient.Do(req)
if err != nil {
    return err
}
defer resp.Body.Close()

// Read streamed SSE response from resp.Body.
```

## Dependencies

This package must not depend on any OpenAI SDK. Depend only on stdlib packages, testify, and other packages implemented in this repo.

Server must be `net/http` compatible.

## Scope and Limitations

- Only Responses API. Only response creation. Only streaming.
- No latency simulation.
- Does not mock hosted tool calls (e.g. OpenAI file search, code execution), reasoning, or MCP calls.

## Matching

- Scan top-to-bottom.
- Optional consume-on-use.
- Partial and exact matching must be supported.

Request fields and header values can either be a string (exact matching) or an object. Example:

```jsonc
{
    "request": {
        // Match if the input field contains "unicorn".
        "input": {"match": "partial", "text": "unicorn"}
    }
}
```

## Public API

```go
// NewHandlerFromFile creates an http.Handler that serves mock OpenAI Responses API requests using response definitions loaded from a JSON or JSON-with-comments
// file.
func NewHandlerFromFile(path string) (http.Handler, error)

// NewHandler creates an http.Handler that serves mock OpenAI Responses API requests using response definitions loaded from JSON or JSON-with-comments bytes.
func NewHandler(data []byte) (http.Handler, error)
```
