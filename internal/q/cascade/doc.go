// Package cascade loads layered configuration into Go structs from multiple sources with predictable precedence.
//
// A Loader builds a prioritized cascade of sources and writes into a destination struct. Register sources from lowest to highest priority using the With* methods, then call StrictlyLoad.
// The zero value of Loader is ready to use; New exists for fluent chaining (ex: New().WithDefaults(...).WithJSONFile(...).WithEnv(...).StrictlyLoad(&cfg)).
//
// Sources
//   - Defaults from a map[string]any whose keys may use dot-notation to denote nesting.
//   - JSON files read at load time. WithJSONFile registers a specific path (absolute or relative). WithNearestJSONFile searches upward from a starting path for the first readable, non-empty
//     file with a given relative name; it panics if fileName is absolute.
//   - Environment variables mapped to configuration keys via WithEnv; missing variables are ignored and present values are strings.
//
// Keys, matching, and coercion Keys are case-insensitive and dot-separated for nesting. Struct field names are matched case-insensitively (ex: "server.port" sets field Server.Port).
// Unknown keys are ignored. Values are coerced when reasonable to the destination type (strings to numbers/bools, numbers to strings, floats to ints truncated toward zero, and slices
// of allowed scalar types). Pointer fields are allocated as needed. Case-insensitive field name collisions are not supported.
//
// Validation and errors Fields tagged cascade:",required" must be set by some source; validation occurs after all sources have been applied. StrictlyLoad returns an error when a readable
// source cannot be parsed or when a value cannot be coerced to the field type; it fails fast and does not continue to later sources to "fix" bad values. Missing or unreadable sources,
// empty/whitespace-only files, and unknown keys do not cause errors. Errors include the source's name for context.
//
// Paths ExpandPath expands a leading "~" to the current user's home directory. InUserConfigDirectory returns an absolute, OS-appropriate path for user-specific config files joined
// with a subpath.
//
// Example
//
//	type Config struct {
//	    Host string `cascade:"required"`
//	    Port int
//	}
//
//	var cfg Config
//	err := New().
//	    WithDefaults(map[string]any{"host": "localhost", "port": 8080}).
//	    WithNearestJSONFile("config/app.json", "").
//	    WithEnv(map[string]string{"host": "APP_HOST", "port": "APP_PORT"}).
//	    StrictlyLoad(&cfg)
//	if err != nil {
//	    // handle error
//	}
package cascade
