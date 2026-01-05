package cli

import (
	"encoding/json"
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
	if err := loader.StrictlyLoad(&cfg); err != nil {
		return Config{}, fmt.Errorf("load configuration: %w", err)
	}
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

	effective := effectiveModel(cfg)
	if _, err := fmt.Fprintf(w, "\nEffective Model: %s\n\n", effective); err != nil {
		return err
	}

	if _, err := fmt.Fprintln(w, "To set LLM provider API keys, set one of these ENV variables:"); err != nil {
		return err
	}
	envVars := llmmodel.ProviderKeyEnvVars()
	for _, pid := range llmmodel.AllProviderIDs {
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

		val := strings.TrimSpace(f.String())
		// A common pattern is to put "***" in config files as a placeholder for
		// secrets; treat that as "unset" so the display can fall back to ENV.
		if isAsteriskPlaceholder(val) {
			val = ""
		}
		if val == "" {
			pid := providerIDFromProviderKeysField(t.Field(i))
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
		if pid == llmmodel.ProviderIDUnknown {
			continue
		}
		llmmodel.ConfigureProviderKey(pid, key)
	}
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
