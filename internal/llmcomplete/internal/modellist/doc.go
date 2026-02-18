// Package modellist provides an unopinionated list of LLM providers and models. It is deliberately simple, only exposing one method:
//
//	modellist.GetProviders()
//
// Again, it is unopinionated and MUST NOT be tied to specific implementations. For example, don't provide recommended values or defaults if it's actually an application-specific
// setting.
//
// In order to update the list:
//   - new providers: add entries in config.go
//   - update: cd cmd/update && go run .
package modellist
