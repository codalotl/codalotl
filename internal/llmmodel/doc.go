// Package llmmodel provides a registry of LLM models, providers, and the metadata needed to select and call them.
//
// The package loads a built-in catalog of user-visible model IDs, provider-side model IDs, supported API shapes, context and output limits, pricing, endpoint URLs,
// and default API-key environment variables. ModelID values are the stable IDs used by application code; provider model IDs are the values sent to provider APIs.
//
// Callers can configure provider API keys, resolve effective keys and endpoints, filter models by available API-key or subscription auth, and register custom model
// aliases or endpoint overrides with AddCustomModel. Environment variable names may be supplied with or without a leading "$".
package llmmodel
