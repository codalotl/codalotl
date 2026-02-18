package main

import (
	"bytes"
	"encoding/json"
	"net/url"
	"sort"
	"strings"
)

// Sanitizer defines a transformation over raw JSON bytes. It should return sanitized JSON bytes. Implementations should be deterministic and preserve semantically
// equivalent formatting (e.g., via re-marshal).
type Sanitizer interface {
	Sanitize(in []byte) ([]byte, error)
}

// SanitizerFunc is an adapter to allow the use of ordinary functions as Sanitizers.
type SanitizerFunc func([]byte) ([]byte, error)

// Sanitize implements Sanitizer.
func (f SanitizerFunc) Sanitize(in []byte) ([]byte, error) { return f(in) }

// ApplySanitizers runs the given sanitizers sequentially.
func ApplySanitizers(in []byte, sanitizers ...Sanitizer) ([]byte, error) {
	out := in
	var err error
	for _, s := range sanitizers {
		out, err = s.Sanitize(out)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// FixProviderData applies ad-hoc overrides to provider JSON by provider id. This lets us correct upstream issues without forking the source schema. Only top-level
// k/v pairs are supported for now.
var FixProviderData Sanitizer = SanitizerFunc(func(in []byte) ([]byte, error) {
	// provider id -> top-level key -> override value
	var overrides = map[string]map[string]interface{}{
		// Upstream sets default_small_model_id to an id that doesn't exist in models.
		// Choose a concrete variant that exists in the models list.
		"huggingface": {
			"default_small_model_id": "openai/gpt-oss-20b:fireworks-ai",
		},
	}

	var m map[string]interface{}
	if err := json.Unmarshal(in, &m); err != nil {
		return in, nil
	}

	idVal, _ := m["id"].(string)
	if idVal == "" {
		return in, nil
	}

	if kv, ok := overrides[idVal]; ok {
		for k, v := range kv {
			m[k] = v
		}
		buf := &bytes.Buffer{}
		enc := json.NewEncoder(buf)
		enc.SetIndent("", "  ")
		if err := enc.Encode(m); err != nil {
			return nil, err
		}
		return bytes.TrimSpace(buf.Bytes()), nil
	}

	return in, nil
})

// RemoveDefaultHeaders removes the top-level "default_headers" key from the provider map if present.
var RemoveDefaultHeaders Sanitizer = SanitizerFunc(func(in []byte) ([]byte, error) {
	var m map[string]interface{}
	if err := json.Unmarshal(in, &m); err != nil {
		return in, nil // If not valid JSON, leave unchanged; upstream will validate separately
	}
	delete(m, "default_headers")
	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(m); err != nil {
		return nil, err
	}
	return bytes.TrimSpace(buf.Bytes()), nil
})

// RenameAPIEndpoint renames "api_endpoint" to either "api_endpoint_url" or "api_endpoint_env" depending on whether the value parses as an absolute URL. If value
// is not a string, it is removed.
var RenameAPIEndpoint Sanitizer = SanitizerFunc(func(in []byte) ([]byte, error) {
	var m map[string]interface{}
	if err := json.Unmarshal(in, &m); err != nil {
		return in, nil
	}
	val, ok := m["api_endpoint"]
	if ok {
		if s, ok := val.(string); ok {
			// Decide URL vs env by parsing
			if u, err := url.Parse(s); err == nil && u.Scheme != "" && u.Host != "" {
				m["api_endpoint_url"] = s
			} else {
				m["api_endpoint_env"] = s
			}
		}
		// Remove original regardless
		delete(m, "api_endpoint")
	}
	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(m); err != nil {
		return nil, err
	}
	return bytes.TrimSpace(buf.Bytes()), nil
})

// SetMissingAPIEndpoint sets a default api_endpoint_url for known providers when missing. It expects the JSON to contain an "id" field with the provider id (e.g.,
// "openai"). If neither "api_endpoint_url" nor "api_endpoint_env" is present, it will set "api_endpoint_url" based on a known mapping. Unknown providers are left
// unchanged.
var SetMissingAPIEndpoint Sanitizer = SanitizerFunc(func(in []byte) ([]byte, error) {
	// Known provider id -> default base URL
	var defaultEndpoints = map[string]string{
		"openai":    "https://api.openai.com/v1",
		"anthropic": "https://api.anthropic.com",
		"gemini":    "https://generativelanguage.googleapis.com",
		"azure":     "", // Azure uses env/instance-specific endpoints; leave empty
		"bedrock":   "", // AWS Bedrock uses regional endpoints
		"vertexai":  "", // GCP Vertex AI uses regional endpoints
	}

	var m map[string]interface{}
	if err := json.Unmarshal(in, &m); err != nil {
		return in, nil
	}

	// If already has a URL, do nothing
	if v, ok := m["api_endpoint_url"]; ok && v != "" {
		return in, nil
	}

	// Read provider id
	idVal, ok := m["id"].(string)
	if !ok || idVal == "" {
		return in, nil
	}

	if urlStr, ok := defaultEndpoints[idVal]; ok && urlStr != "" {
		m["api_endpoint_url"] = urlStr
		buf := &bytes.Buffer{}
		enc := json.NewEncoder(buf)
		enc.SetIndent("", "  ")
		if err := enc.Encode(m); err != nil {
			return nil, err
		}
		return bytes.TrimSpace(buf.Bytes()), nil
	}

	return in, nil
})

// SetIsLegacy marks certain models as legacy by adding "is_legacy": true on the model object. Rules:
//   - openai: any model id starting with "gpt-4" (e.g., gpt-4.1, gpt-4o, gpt-4o-mini)
//   - anthropic: any model id starting with "claude-sonnet-4" or "claude-opus-4" (e.g., 4, 4.1, 4.5 variants)
//   - xai: any model id starting with "grok-3" (e.g., grok-3, grok-3-mini)
//
// For non-legacy models, the key is removed if present to keep output minimal.
var SetIsLegacy Sanitizer = SanitizerFunc(func(in []byte) ([]byte, error) {
	var m map[string]interface{}
	if err := json.Unmarshal(in, &m); err != nil {
		return in, nil
	}

	providerID, _ := m["id"].(string)
	if providerID == "" {
		return in, nil
	}

	rawModels, ok := m["models"].([]interface{})
	if !ok || len(rawModels) == 0 {
		return in, nil
	}

	isLegacyStart := func(modelID string) bool {
		switch providerID {
		case "openai":
			return !strings.HasPrefix(modelID, "gpt-5")
		case "anthropic":
			return modelID == "claude-opus-4-20250514"
		case "xai":
			return strings.HasPrefix(modelID, "grok-3")
		default:
			return false
		}
	}

	inIsLegacyRegion := false
	for i := range rawModels {
		mv, ok := rawModels[i].(map[string]interface{})
		if !ok {
			continue
		}
		mid, _ := mv["id"].(string)

		inIsLegacyRegion = inIsLegacyRegion || isLegacyStart(mid)

		if inIsLegacyRegion {
			mv["is_legacy"] = true
		}
		rawModels[i] = mv
	}
	m["models"] = rawModels

	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(m); err != nil {
		return nil, err
	}
	return bytes.TrimSpace(buf.Bytes()), nil
})

// OrderKeys reorders JSON keys to match our struct field order for providers and models. Unknown keys are preserved and appended at the end in lexicographic order.
var OrderKeys Sanitizer = SanitizerFunc(func(in []byte) ([]byte, error) {
	var m map[string]interface{}
	if err := json.Unmarshal(in, &m); err != nil {
		// If not valid JSON, leave unchanged
		return in, nil
	}

	// Known order based on modellist.Provider struct field tags
	providerOrder := []string{
		"name",
		"id",
		"type",
		"api_endpoint_url",
		"api_key",
		"api_endpoint_env",
		"default_large_model_id",
		"default_small_model_id",
		"models",
	}

	// If models present, ensure each model object has ordered keys
	if rawModels, ok := m["models"].([]interface{}); ok {
		for i := range rawModels {
			if modelMap, ok := rawModels[i].(map[string]interface{}); ok {
				// Known order based on modellist.Model struct field tags
				modelOrder := []string{
					"id",
					"name",
					"is_legacy",
					"cost_per_1m_in",
					"cost_per_1m_out",
					"cost_per_1m_in_cached",
					"cost_per_1m_out_cached",
					"context_window",
					"default_max_tokens",
					"can_reason",
					"has_reasoning_efforts",
					"supports_attachments",
				}
				// Serialize back to ordered object bytes and then reparse to preserve ordering when we emit manually below
				var buf bytes.Buffer
				if err := writeObjectOrderedPretty(&buf, modelMap, modelOrder, 0); err != nil {
					return nil, err
				}
				var reparsed map[string]interface{}
				if err := json.Unmarshal(buf.Bytes(), &reparsed); err == nil {
					rawModels[i] = reparsed
				}
			}
		}
		m["models"] = rawModels
	}

	// Write provider with ordered keys and pretty formatting
	var outBuf bytes.Buffer
	if err := writeObjectOrderedPretty(&outBuf, m, providerOrder, 0); err != nil {
		return nil, err
	}
	return bytes.TrimSpace(outBuf.Bytes()), nil
})

// writeObjectOrderedPretty writes a JSON object with keys ordered by desiredOrder (only those present), followed by any remaining keys in lexicographic order. Pretty
// prints with two-space indentation.
func writeObjectOrderedPretty(buf *bytes.Buffer, obj map[string]interface{}, desiredOrder []string, level int) error {
	keysInObj := make(map[string]struct{}, len(obj))
	for k := range obj {
		keysInObj[k] = struct{}{}
	}

	ordered := make([]string, 0, len(obj))
	for _, k := range desiredOrder {
		if _, ok := keysInObj[k]; ok {
			ordered = append(ordered, k)
			delete(keysInObj, k)
		}
	}
	// Collect unknown keys and sort for determinism
	unknown := make([]string, 0, len(keysInObj))
	for k := range keysInObj {
		unknown = append(unknown, k)
	}
	sort.Strings(unknown)
	ordered = append(ordered, unknown...)

	indent := func(n int) string { return strings.Repeat("  ", n) }

	buf.WriteByte('{')
	if len(ordered) > 0 {
		buf.WriteByte('\n')
	}
	for i, k := range ordered {
		buf.WriteString(indent(level + 1))
		// key
		kb, _ := json.Marshal(k)
		buf.Write(kb)
		buf.WriteString(": ")
		// value
		v := obj[k]
		if k == "models" {
			// Special pretty handling for models array
			if arr, ok := v.([]interface{}); ok {
				if err := writeModelsArrayPretty(buf, arr, level+1); err != nil {
					return err
				}
			} else {
				vb, _ := json.Marshal(v)
				buf.Write(vb)
			}
		} else if mv, ok := v.(map[string]interface{}); ok {
			// Generic nested map; write compactly to keep logic simple
			if err := writeObjectOrderedPretty(buf, mv, nil, level+1); err != nil {
				return err
			}
		} else {
			vb, _ := json.Marshal(v)
			buf.Write(vb)
		}
		if i < len(ordered)-1 {
			buf.WriteByte(',')
		}
		buf.WriteByte('\n')
	}
	if len(ordered) > 0 {
		buf.WriteString(indent(level))
	}
	buf.WriteByte('}')
	return nil
}

func writeModelsArrayPretty(buf *bytes.Buffer, arr []interface{}, level int) error {
	indent := func(n int) string { return strings.Repeat("  ", n) }
	buf.WriteByte('[')
	if len(arr) > 0 {
		buf.WriteByte('\n')
	}
	for i, elem := range arr {
		buf.WriteString(indent(level + 1))
		if mv, ok := elem.(map[string]interface{}); ok {
			// Use model field order
			modelOrder := []string{
				"id",
				"name",
				"is_legacy",
				"cost_per_1m_in",
				"cost_per_1m_out",
				"cost_per_1m_in_cached",
				"cost_per_1m_out_cached",
				"context_window",
				"default_max_tokens",
				"can_reason",
				"has_reasoning_efforts",
				"supports_attachments",
			}
			if err := writeObjectOrderedPretty(buf, mv, modelOrder, level+1); err != nil {
				return err
			}
		} else {
			vb, _ := json.Marshal(elem)
			buf.Write(vb)
		}
		if i < len(arr)-1 {
			buf.WriteByte(',')
		}
		buf.WriteByte('\n')
	}
	if len(arr) > 0 {
		buf.WriteString(indent(level))
	}
	buf.WriteByte(']')
	return nil
}
