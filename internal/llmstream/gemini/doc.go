// Package gemini implements a small, genai-like client for Gemini streaming REST calls.
//
// Compatibility with google.golang.org/genai
//
// This package is intentionally a smaller subset, not a drop-in replacement for google.golang.org/genai. It is focused on the streaming generate-content flow needed
// by llmstream.
//
// Intentional omissions include:
//   - Vertex AI configuration and authentication.
//   - Extra HTTPOptions features beyond BaseURL and Headers.
//   - GenerateContentConfig fields not exposed by this package.
//   - Part variants not represented by Part.
//   - Richer response metadata not represented by GenerateContentResponse and related types.
//   - Extra FunctionResponse fields such as Scheduling, WillContinue, and Parts.
package gemini
