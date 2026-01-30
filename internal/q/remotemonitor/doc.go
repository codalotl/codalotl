// Package remotemonitor embeds a lightweight monitor into CLI binaries to check for newer versions and to report anonymous
// operational signals—events, errors, and panics—to a server you control.
//
// Create a Monitor with NewMonitor(version, host). Host is the base URL (scheme required). Configure endpoints by setting
// LatestVersionURL (defaults to "/version" and may be an absolute URL suitable for a CDN/object store), ReportErrorPath
// (ex: "/report_error"), ReportPanicPath (ex: "/report_panic"), and ReportEventPath (ex: "/report_event"). Use SetStableProperties
// to supply caller-defined key/value pairs that are included with error and panic reports and, optionally, with events.
//
// Version checks expect the latest-version endpoint to return JSON of the form {"version":"1.2.3"}.
// They can be launched asynchronously with FetchLatestVersionFromHost, queried synchronously with LatestVersionSync
// (which deduplicates concurrent requests and caches results), or read from cache with LatestVersionAsync, which returns
// ErrNotCached when no value is available.
//
// Reporting APIs send:
//   - ReportError: POST to Host+ReportErrorPath with the error string, optional metadata, host properties, and stable properties.
//   - ReportPanic: POST to Host+ReportPanicPath with the panic value, stack, optional metadata, host properties, and stable
//     properties.
//   - ReportEventAsync: GET to Host+ReportEventPath with event and metadata; stable properties are included only when requested.
//
// All Monitor methods are safe for concurrent use. The HTTP client defaults to a 4-second timeout and is created lazily;
// replace m.httpClient before use to customize transport or timeouts with a client that is safe for concurrent use.
//
// Privacy: HostProperties and default payloads avoid PII. Do not include usernames, IP/MAC addresses, file paths, environment
// dumps, or similar sensitive data in metadata or stable properties.
package remotemonitor
