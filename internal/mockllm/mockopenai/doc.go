// Package mockopenai provides an http.Handler that mocks the streaming OpenAI Responses API for tests.
//
// The handler accepts POST requests to /responses and /v1/responses. It matches requests against a configured list of responses from top to bottom and serves the
// first match as a server-sent event (SSE) stream that ends with `data: [DONE]`.
//
// Configuration is loaded from JSON or JSON-with-comments. Line comments, block comments, and trailing commas are allowed so test fixtures can use JSONC.
//
// The top-level configuration shape is:
//
//	{
//	  "responses": [
//	    {
//	      "name": "optional label for error messages",
//	      "consume": true,
//	      "request": {
//	        "model": "gpt-5.4",
//	        "input": {"match": "partial", "text": "unicorn"}
//	      },
//	      "headers": [
//	        {"name": "Authorization", "value": {"match": "partial", "text": "Bearer"}}
//	      ],
//	      "response": {
//	        "id": "resp_123",
//	        "object": "response"
//	      }
//	    }
//	  ]
//	}
//
// Request fields and header values support three matching modes:
//
//   - a JSON string for exact text matching
//   - {"match":"partial","text":"..."} to match a substring
//   - {"match":"partial","texts":["alpha","beta"]} to require multiple substrings in order without overlap
//
// Non-string matcher values are compared by JSON structure, so objects and arrays can be matched exactly as JSON values.
//
// Responses with `consume: true` can be used only once. Tests that expect all such responses to be exercised should call AssertAllConsumed after the code under
// test finishes.
//
// Typical usage is to build a handler with NewHandler or NewHandlerFromFile and serve it with httptest.NewServer.
package mockopenai
