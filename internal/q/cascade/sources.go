package cascade

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// cascadeSource represents a configuration source that can supply key/value data to the loader in a normalized map form.
type cascadeSource interface {
	// Name returns a human-readable label for the source, used in error messages and diagnostics.
	Name() string

	// ToMap returns a normalized map:
	//   - keys are lower cased
	//   - keys have no "." -- those are expansion operators that create nested maps
	//   - values are one of: normalized nested maps (ONLY via map[string]any -- not map[string]string or any other); scalars (ONLY int, float64, bool, string); slices of the aformentioned;
	//     nil.
	//
	// Errors are returned when:
	//   - reading/parsing fails
	//   - cannot create unambiguous normalized map
	ToMap() (map[string]any, error)
}

// sourceMap adapts a Go map into a cascadeSource. Keys may use dot-notation to create nested objects and are normalized to lower case at merge time.
type sourceMap struct {
	isDefaults bool           // When true, Name reports "Defaults"; otherwise it reports "Go Map".
	m          map[string]any // Raw input map to normalize. Keys may include "." for nesting; nested objects must be map[string]any; values must be allowed normalized types.
}

// sourceJSONFile implements cascadeSource for a single JSON file whose contents are read and normalized at load time. Empty or whitespace-only files contribute no values.
type sourceJSONFile struct {
	path string // Path to the JSON file to read at load time. May be absolute or relative and is expanded with ExpandPath.
}

// sourceEnv implements cascadeSource backed by environment variables mapped to configuration keys.
type sourceEnv struct {
	// envToKey maps a key in a map ("." allowed for nesting) to an ENV variable. Ex: {"server.host": "SERVER_HOST", "server.port": "SERVER_PORT"} creates {server: {host: "example.com",
	// port: 1234}}, assuming the env variables are set.
	envToKey map[string]string
}

// Name implements cascadeSource. It returns "Defaults" when this source represents defaults; otherwise it returns "Go Map".
func (s *sourceMap) Name() string {
	if s.isDefaults {
		return "Defaults"
	}
	return "Go Map"
}

// ToMap normalizes s.m into a lowercased, nested map suitable for application to a destination struct. Dotted keys are expanded into nested objects, nested map[string]any values are
// deep-merged, and values are validated to be allowed normalized types. If s.m is nil, it returns an empty map. It returns an error on key conflicts or invalid value types.
func (s *sourceMap) ToMap() (map[string]any, error) {
	if s.m == nil {
		return map[string]any{}, nil
	}

	normalizedMap := map[string]any{}

	for k, v := range s.m {
		parts := strings.Split(k, ".")
		if err := mergeIntoObject(normalizedMap, parts, v, k); err != nil {
			return nil, err
		}
	}

	return normalizedMap, nil
}

// mergeIntoObject inserts value into obj along parts (a path of key segments), lowercasing each segment. If value is a map[string]any at the leaf, it is deep-merged; otherwise value
// must be an allowed normalized type. It returns an error for invalid keys, invalid value types, or key conflicts (e.g., when a non-object already exists at an intermediate segment,
// or a leaf is set twice). fullKey is used only to annotate errors. obj is modified in place.
func mergeIntoObject(obj map[string]any, parts []string, value any, fullKey string) error {
	if len(parts) == 0 {
		return fmt.Errorf("invalid key")
	}
	part := strings.ToLower(parts[0])
	if len(parts) == 1 {
		// Last segment:
		if mv, ok := value.(map[string]any); ok {
			// Ensure destination is a map and merge into it
			existing, exists := obj[part]
			if exists {
				destMap, isMap := existing.(map[string]any)
				if !isMap {
					return fmt.Errorf("key conflict: key '%s' was already set", fullKey)
				}
				return mergeMap(destMap, mv, fullKey)
			}
			destMap := map[string]any{}
			obj[part] = destMap
			return mergeMap(destMap, mv, fullKey)
		}

		// Scalar/leaf value
		if err := validateAllowedValue(value); err != nil {
			return fmt.Errorf("invalid value for key '%s': %w", fullKey, err)
		}
		if existing, exists := obj[part]; exists {
			if _, isMap := existing.(map[string]any); isMap {
				return fmt.Errorf("key conflict: key '%s' was set to an object", fullKey)
			}
			return fmt.Errorf("key conflict: key '%s' was already set", fullKey)
		}
		obj[part] = value
		return nil
	}

	// Intermediate segment:
	existing, exists := obj[part]
	if exists {
		if m, ok := existing.(map[string]any); ok {
			return mergeIntoObject(m, parts[1:], value, fullKey)
		}
		return fmt.Errorf("key conflict at '%s': '%s' is not an object", fullKey, part)
	}
	child := map[string]any{}
	obj[part] = child
	return mergeIntoObject(child, parts[1:], value, fullKey)
}

// mergeMap merges all entries from src into dest, lowercasing keys and expanding dotted keys into nested objects via mergeIntoObject. baseKey, when non-empty, is prefixed to error
// paths for context. It returns the first error encountered. dest is modified in place; src is not mutated.
func mergeMap(dest map[string]any, src map[string]any, baseKey string) error {
	for k, v := range src {
		kLower := strings.ToLower(k)
		subParts := strings.Split(kLower, ".")
		full := kLower
		if baseKey != "" {
			full = baseKey + "." + kLower
		}
		if err := mergeIntoObject(dest, subParts, v, full); err != nil {
			return err
		}
	}
	return nil
}

// validateAllowedValue validates that v is one of the allowed normalized types:
//   - nil
//   - scalars: int, float64, bool, string
//   - slices: []int, []float64, []bool, []string
//   - slices of objects: []map[string]any, with recursively validated values
func validateAllowedValue(v any) error {
	switch vv := v.(type) {
	case nil:
		return nil
	case int, float64, bool, string:
		return nil
	case []int, []float64, []bool, []string:
		return nil
	case []map[string]any:
		for i, m := range vv {
			for mk, mv := range m {
				if err := validateAllowedValue(mv); err != nil {
					return fmt.Errorf("invalid nested value in object[%d] at key '%s': %w", i, mk, err)
				}
			}
		}
		return nil
	default:
		return fmt.Errorf("type %T is not allowed", v)
	}
}

// Name implements cascadeSource and returns a human-readable label that includes the file path, e.g., "JSON File: <path>".
func (s *sourceJSONFile) Name() string {
	return fmt.Sprintf("JSON File: %s", s.path)
}

// Implements cascadeSource.
//
// NOTE: empty arrays are considered []string{}.
func (s *sourceJSONFile) ToMap() (map[string]any, error) {
	if s == nil || s.path == "" {
		return map[string]any{}, nil
	}

	data, err := os.ReadFile(ExpandPath(s.path))
	if err != nil {
		// TODO: determine if err is not found or permission (not errors)
		return nil, fmt.Errorf("read json file: %w", err)
	}

	// Treat empty/whitespace-only files as empty config.
	if strings.TrimSpace(string(data)) == "" {
		return map[string]any{}, nil
	}

	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse json: %w", err)
	}

	obj, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("top-level JSON must be an object")
	}

	normalized, err := normalizeJSONMap(obj)
	if err != nil {
		return nil, err
	}

	dest := map[string]any{}
	if err := mergeMap(dest, normalized, ""); err != nil {
		return nil, err
	}
	return dest, nil
}

// normalizeJSONMap normalizes a JSON-decoded map by recursively normalizing values into allowed scalar/slice/object forms used by this package.
func normalizeJSONMap(m map[string]any) (map[string]any, error) {
	out := make(map[string]any, len(m))
	for k, v := range m {
		nv, err := normalizeJSONValue(v)
		if err != nil {
			return nil, fmt.Errorf("key '%s': %w", k, err)
		}
		out[k] = nv
	}
	return out, nil
}

// normalizeJSONValue converts JSON-decoded values (which use []any for arrays and float64 for numbers) into the strictly allowed types defined by validateAllowedValue.
func normalizeJSONValue(v any) (any, error) {
	switch vv := v.(type) {
	case nil:
		return nil, nil
	case string, bool, float64:
		return vv, nil
	case map[string]any:
		return normalizeJSONMap(vv)
	case []any:
		if len(vv) == 0 {
			return []string{}, nil
		}
		switch vv[0].(type) {
		case string:
			out := make([]string, len(vv))
			for i, e := range vv {
				s, ok := e.(string)
				if !ok {
					return nil, fmt.Errorf("array contains mixed types (expected string)")
				}
				out[i] = s
			}
			return out, nil
		case bool:
			out := make([]bool, len(vv))
			for i, e := range vv {
				b, ok := e.(bool)
				if !ok {
					return nil, fmt.Errorf("array contains mixed types (expected bool)")
				}
				out[i] = b
			}
			return out, nil
		case float64:
			out := make([]float64, len(vv))
			for i, e := range vv {
				f, ok := e.(float64)
				if !ok {
					return nil, fmt.Errorf("array contains mixed types (expected number)")
				}
				out[i] = f
			}
			return out, nil
		case map[string]any:
			out := make([]map[string]any, len(vv))
			for i, e := range vv {
				m, ok := e.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("array contains mixed types (expected object)")
				}
				nm, err := normalizeJSONMap(m)
				if err != nil {
					return nil, fmt.Errorf("array object[%d]: %w", i, err)
				}
				out[i] = nm
			}
			return out, nil
		default:
			return nil, fmt.Errorf("unsupported array element type %T", vv[0])
		}
	default:
		return nil, fmt.Errorf("unsupported type %T", v)
	}
}

// Name implements cascadeSource and identifies the source as "ENV".
func (s *sourceEnv) Name() string {
	return "ENV"
}

// Implements cascadeSource.
//
// Missing env variables do not set any key. All values are strings.
func (s *sourceEnv) ToMap() (map[string]any, error) {
	if s == nil || s.envToKey == nil {
		return map[string]any{}, nil
	}

	out := map[string]any{}
	for mapKey, envVar := range s.envToKey {
		if envVar == "" {
			// No ENV var to read for this key.
			continue
		}
		val, exists := os.LookupEnv(envVar)
		if !exists {
			// Missing env variables do not set any key.
			continue
		}
		if val == "" {
			// The case can be made that "" is meaningful in some situations.
			// However, I was just bitten by an "empty env var" that was overwriting settings in a JSON file.
			// Can always revisit this.
			continue
		}
		parts := strings.Split(mapKey, ".")
		if err := mergeIntoObject(out, parts, val, mapKey); err != nil {
			return nil, err
		}
	}
	return out, nil
}
