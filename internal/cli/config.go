package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/q/cascade"
)

// Config is codalotl's configuration loaded from a cascade of sources.
//
// NOTE: internal/q/cascade matches keys to struct field names case-insensitively;
// it does not use json tags. The json tags are for `codalotl config` output and
// for compatibility with typical config.json naming.
type Config struct {
	ProviderKeys ProviderKeys `json:"providerkeys"`

	// MaxWidth is the max width when reflowing documentation.
	// Defaults to 120.
	MaxWidth           int                `json:"maxwidth"`
	MaxWidthProvidence cascade.Providence `json:"-"`

	DisableTelemetry      bool `json:"disabletelemetry,omitempty"`
	DisableCrashReporting bool `json:"disablecrashreporting,omitempty"`

	// Optional. If set, use this provider if possible (lower precedence than
	// PreferredModel, though).
	PreferredProvider string `json:"preferredprovider"`

	// Optional. If set, use this model specifically.
	PreferredModel string `json:"preferredmodel"`
	// PreferredModelProvidence indicates which source set PreferredModel, when
	// any source actually did. This is used to decide which config file should
	// be updated if the TUI asks to persist a newly selected model.
	PreferredModelProvidence cascade.Providence `json:"-"`

	// configLocations are the JSON config file paths that actually contributed
	// values during load (low->high precedence). This is intentionally not part
	// of the user-visible JSON schema.
	configLocations []string
}

// ProviderKeys is kept separate so tests can easily validate its zero value.
type ProviderKeys struct {
	OpenAI string `json:"openai"`

	// NOTE: in the future, we may add these:
	// Anthropic string `json:"anthropic"`
	// XAI       string `json:"xai"`
	// Gemini    string `json:"gemini"`
}

func loadConfig() (Config, error) {
	loader := cascade.New().WithDefaults(map[string]any{
		"maxwidth": 120,
	})

	// Global user config.
	//
	// We register at a lower precedence than the nearest  project config so local config can override global config.
	homeCfg := cascade.ExpandPath("~/.codalotl/config.json")
	loader = loader.WithJSONFile(homeCfg)

	// Local project config (highest precedence of the built-ins).
	loader = loader.WithNearestJSONFile(filepath.Join(".codalotl", "config.json"), "")

	var cfg Config
	report, err := loader.StrictlyLoadWithReport(&cfg)
	if err != nil {
		return Config{}, fmt.Errorf("load configuration: %w", err)
	}
	cfg.configLocations = configLocationsFromReport(report)
	if err := validateConfig(cfg); err != nil {
		return Config{}, err
	}
	// Apply provider key overrides from config so llmmodel can resolve keys with
	// the right precedence (config overrides env defaults).
	configureProviderKeysFromConfig(cfg.ProviderKeys)
	return cfg, nil
}

func validateConfig(cfg Config) error {
	if cfg.MaxWidth <= 0 {
		return fmt.Errorf("invalid configuration: maxwidth must be > 0 (got %d)", cfg.MaxWidth)
	}
	return nil
}

func writeConfigJSON(w io.Writer, cfg Config) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(cfg); err != nil {
		return err
	}
	return nil
}

func writeConfig(w io.Writer, cfg Config) error {
	if err := writeStringln(w, "Current Configuration:"); err != nil {
		return err
	}

	displayCfg := cfg
	displayCfg.ProviderKeys = providerKeysForDisplay(cfg.ProviderKeys)

	if err := writeConfigJSON(w, displayCfg); err != nil {
		return err
	}

	locs := cfg.configLocations
	if len(locs) == 0 {
		if _, err := fmt.Fprintln(w, "\nCurrent Config Location(s): (none - using defaults and ENV)"); err != nil {
			return err
		}
	} else if len(locs) == 1 {
		if _, err := fmt.Fprintf(w, "\nCurrent Config Location(s): %s\n", locs[0]); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintln(w, "\nCurrent Config Location(s):"); err != nil {
			return err
		}
		for _, p := range locs {
			if _, err := fmt.Fprintf(w, "- %s\n", p); err != nil {
				return err
			}
		}
	}

	effective := effectiveModel(cfg)
	if _, err := fmt.Fprintf(w, "\nEffective Model: %s\n\n", effective); err != nil {
		return err
	}

	if _, err := fmt.Fprintln(w, "To set LLM provider API keys, set one of these ENV variables:"); err != nil {
		return err
	}
	envVars := llmmodel.ProviderKeyEnvVars()
	for _, pid := range providerIDsExposedByProviderKeys() {
		if !isKnownProviderID(pid) {
			continue
		}
		ev := strings.TrimSpace(envVars[pid])
		if ev == "" {
			continue
		}
		if _, err := fmt.Fprintf(w, "- %s\n", ev); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Global configuration can be stored in %s\n", globalConfigPath()); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "Project-specific configuration can be stored in .codalotl/config.json"); err != nil {
		return err
	}

	return nil
}

func globalConfigPath() string {
	// Keep this consistent with loadConfig's built-in search paths so the output
	// is actionable.
	return cascade.ExpandPath("~/.codalotl/config.json")
}

func configLocationsFromReport(report cascade.LoadReport) []string {
	var out []string
	for _, src := range report.Sources {
		if src.SourceType != "json_file" {
			continue
		}
		p := strings.TrimSpace(src.SourceIdentifier)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

func effectiveModel(cfg Config) llmmodel.ModelID {
	// This is informational only today, but it's still useful to show what a
	// "no config" installation would do by default.
	pm := llmmodel.ModelID(strings.TrimSpace(cfg.PreferredModel))
	if pm != "" {
		return llmmodel.ModelIDOrFallback(pm)
	}

	pp := llmmodel.ProviderID(strings.TrimSpace(cfg.PreferredProvider))
	if pp != "" {
		return llmmodel.ModelIDOrFallback(pp.DefaultModel())
	}

	return llmmodel.ModelIDOrFallback("")
}

func providerKeysForDisplay(pk ProviderKeys) ProviderKeys {
	out := pk

	// Use reflection so new providers added to ProviderKeys are automatically
	// redacted without touching this code again.
	v := reflect.ValueOf(&out).Elem()
	t := v.Type()

	envVars := llmmodel.ProviderKeyEnvVars()

	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		if f.Kind() != reflect.String || !f.CanSet() {
			continue
		}

		pid := providerIDFromProviderKeysField(t.Field(i))
		// Only display provider keys for providers that are both exposed by the
		// CLI config schema and supported by llmmodel.
		if pid == llmmodel.ProviderIDUnknown || !isKnownProviderID(pid) {
			f.SetString("")
			continue
		}

		val := strings.TrimSpace(f.String())
		// A common pattern is to put "***" in config files as a placeholder for
		// secrets; treat that as "unset" so the display can fall back to ENV.
		if isAsteriskPlaceholder(val) {
			val = ""
		}
		if val == "" {
			if ev := strings.TrimSpace(envVars[pid]); ev != "" {
				val = strings.TrimSpace(os.Getenv(ev))
			}
		}

		if val == "" {
			f.SetString("")
			continue
		}
		f.SetString(redactSecret(val))
	}

	return out
}

func configureProviderKeysFromConfig(pk ProviderKeys) {
	// Use reflection so new providers added to ProviderKeys are automatically
	// supported without touching this code again.
	v := reflect.ValueOf(pk)
	t := v.Type()

	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		if f.Kind() != reflect.String {
			continue
		}
		key := strings.TrimSpace(f.String())
		// Treat "***" patterns as unset so we don't accidentally configure an
		// invalid placeholder key.
		if isAsteriskPlaceholder(key) {
			continue
		}
		if key == "" {
			continue
		}

		pid := providerIDFromProviderKeysField(t.Field(i))
		if pid == llmmodel.ProviderIDUnknown || !isKnownProviderID(pid) {
			continue
		}
		llmmodel.ConfigureProviderKey(pid, key)
	}
}

func providerIDsExposedByProviderKeys() []llmmodel.ProviderID {
	// The json tags on ProviderKeys define the user-visible config schema.
	// We use reflection so that adding/removing a provider in ProviderKeys
	// automatically updates the list here.
	t := reflect.TypeOf(ProviderKeys{})

	out := make([]llmmodel.ProviderID, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		pid := providerIDFromProviderKeysField(t.Field(i))
		if pid == llmmodel.ProviderIDUnknown {
			continue
		}
		out = append(out, pid)
	}
	return out
}

func isKnownProviderID(pid llmmodel.ProviderID) bool {
	if pid == llmmodel.ProviderIDUnknown {
		return false
	}
	for _, known := range llmmodel.AllProviderIDs {
		if known == pid {
			return true
		}
	}
	return false
}

func isAsteriskPlaceholder(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] != '*' {
			return false
		}
	}
	return true
}

func providerIDFromProviderKeysField(sf reflect.StructField) llmmodel.ProviderID {
	tag := strings.TrimSpace(sf.Tag.Get("json"))
	if tag == "" || tag == "-" {
		return llmmodel.ProviderIDUnknown
	}
	if comma := strings.Index(tag, ","); comma >= 0 {
		tag = tag[:comma]
	}
	tag = strings.TrimSpace(tag)
	if tag == "" || tag == "-" {
		return llmmodel.ProviderIDUnknown
	}
	return llmmodel.ProviderID(tag)
}

func redactSecret(s string) string {
	if s == "" {
		return ""
	}
	// Elide middle section, keep short hint of prefix/suffix if long enough.
	if len(s) <= 8 {
		return strings.Repeat("*", len(s))
	}
	prefix := s[:4]
	suffix := s[len(s)-4:]
	return prefix + "..." + suffix
}

func persistPreferredModelID(cfg Config, newModelID llmmodel.ModelID) error {
	path := configPathForPreferredModelPersistence(cfg)
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("persist preferred model: no config path available")
	}
	if err := setPreferredModelInConfigFile(path, newModelID); err != nil {
		return fmt.Errorf("persist preferred model: %w", err)
	}
	return nil
}

func configPathForPreferredModelPersistence(cfg Config) string {
	// If some config file explicitly set PreferredModel, update that same file.
	if cfg.PreferredModelProvidence.IsSet() &&
		cfg.PreferredModelProvidence.SourceType == "json_file" &&
		strings.TrimSpace(cfg.PreferredModelProvidence.SourceIdentifier) != "" {
		return strings.TrimSpace(cfg.PreferredModelProvidence.SourceIdentifier)
	}

	// Otherwise, pick the highest-precedence config file that contributed any
	// config values.
	if n := len(cfg.configLocations); n > 0 {
		return cfg.configLocations[n-1]
	}

	// If there were no contributing config files, fall back to the global config.
	return globalConfigPath()
}

func setPreferredModelInConfigFile(path string, newModelID llmmodel.ModelID) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create config dir %q: %w", dir, err)
	}

	// Preserve existing file mode when possible.
	mode := os.FileMode(0644)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode() & 0777
	}

	obj, err := readJSONObjectFile(path)
	if err != nil {
		return err
	}

	key := "preferredmodel"
	if strings.TrimSpace(string(newModelID)) == "" {
		delete(obj, key)
	} else {
		obj[key] = string(newModelID)
	}

	if err := writeJSONObjectFileAtomic(path, obj, mode); err != nil {
		return err
	}
	return nil
}

func readJSONObjectFile(path string) (map[string]any, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, fmt.Errorf("open %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	dec := json.NewDecoder(f)
	var obj map[string]any
	if err := dec.Decode(&obj); err != nil {
		// Treat empty files like an empty object for convenience.
		if errors.Is(err, io.EOF) {
			return map[string]any{}, nil
		}
		return nil, fmt.Errorf("parse %q: %w", path, err)
	}
	if obj == nil {
		obj = map[string]any{}
	}
	return obj, nil
}

func writeJSONObjectFileAtomic(path string, obj map[string]any, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "config.*.json")
	if err != nil {
		return fmt.Errorf("create temp file in %q: %w", dir, err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	defer cleanup()

	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(obj); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp config: %w", err)
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		return fmt.Errorf("chmod temp config: %w", err)
	}

	// On Unix, Rename replaces atomically; on Windows it fails if dest exists.
	if err := os.Rename(tmpName, path); err != nil {
		if os.IsExist(err) {
			_ = os.Remove(path)
			if err2 := os.Rename(tmpName, path); err2 == nil {
				_ = os.Chmod(path, mode)
				return nil
			}
		}
		return fmt.Errorf("replace %q: %w", path, err)
	}
	_ = os.Chmod(path, mode)
	// Rename succeeded; don't delete the temp file.
	cleanup = func() {}
	return nil
}
