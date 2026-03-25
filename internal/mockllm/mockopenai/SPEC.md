# mockopenai

The `mockopenai` package implements a mock HTTP server for a subset of the OpenAI API, for the purpose of testing.

Inputs/outputs are given via a JSON file. The response is streamed using SSE.

## Example Usage

Given a JSON or JSON-with-comments file:

```jsonc
{
    "responses": [
        {
            // name is optional metadata only
            "name": "initial request",

            // if true, if this request is used, it cannot be matched again.
            "consume": false,

            // Request fields to /responses
            "request": {
                "model": "gpt-5.4",
                "input": "Tell me a three sentence bedtime story about a unicorn."
            },

            // headers: optional request headers
            "headers": [
                { "name": "X-Tenant-ID", "value": "tenant-a", "match_type": "exact" },
                { "name": "Authorization", "value": "Bearer", "match_type": "contains" }
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

TODO: put in example usage in Go of how we set up server and actually use this library.

## Dependencies

This package must not depend on any OpenAI SDK. Only depend on stdlib packages, testify, and other packages implemented in this repo.

Server must be `net/http` compatible.

## Scope and Limitations

- Only Responses API. Only response creation. Only streaming.
- No latency simulation
- Does not mock hosted tools (e.g. OpenAI file search, code execution) calls, reasoning, and MCP calls

## Matching

- Scan top-to-bottom.
- Optional consume-on-use.
- Partial and exact matching must be supported

Fields in the request can either be a string (exact matching) or an object. Example:

```jsonc
{
    "request": {
        // Match if the input field contains "unicorn".
        "input": {"match": "partial", "text": "unicorn"}
    }
}
```
