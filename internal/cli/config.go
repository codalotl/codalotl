package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

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
	OpenAI    string `json:"openai"`
	Anthropic string `json:"anthropic"`
	XAI       string `json:"xai"`
	Gemini    string `json:"gemini"`
}

func loadConfig() (Config, error) {
	loader := cascade.New().WithDefaults(map[string]any{
		"maxwidth": 120,
	})

	// Global user config.
	//
	// Spec preference is "~/.codalotl/config.json" or (Windows) "%LOCALAPPDATA%\\.codalotl\\config.json".
	// We register both (when meaningful) at a lower precedence than the nearest
	// project config so local config can override global config.
	homeCfg := cascade.ExpandPath("~/.codalotl/config.json")
	loader = loader.WithJSONFile(homeCfg)

	if runtime.GOOS == "windows" {
		if lad := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); lad != "" {
			loader = loader.WithJSONFile(filepath.Join(lad, ".codalotl", "config.json"))
		}
	}

	// Local project config (highest precedence of the built-ins).
	loader = loader.WithNearestJSONFile(filepath.Join(".codalotl", "config.json"), "")

	var cfg Config
	if err := loader.StrictlyLoad(&cfg); err != nil {
		return Config{}, fmt.Errorf("load configuration: %w", err)
	}
	if err := validateConfig(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func validateConfig(cfg Config) error {
	if cfg.MaxWidth <= 0 {
		return fmt.Errorf("invalid configuration: maxwidth must be > 0 (got %d)", cfg.MaxWidth)
	}
	if err := validateOpenAIOnly(cfg); err != nil {
		return err
	}
	return nil
}

// validateOpenAIOnly enforces a temporary limitation: for now codalotl only
// supports OpenAI.
//
// Keep this separate from the main validation so it's easy to remove later.
func validateOpenAIOnly(cfg Config) error {
	var offenders []string
	if strings.TrimSpace(cfg.ProviderKeys.Anthropic) != "" {
		offenders = append(offenders, "providerkeys.anthropic")
	}
	if strings.TrimSpace(cfg.ProviderKeys.XAI) != "" {
		offenders = append(offenders, "providerkeys.xai")
	}
	if strings.TrimSpace(cfg.ProviderKeys.Gemini) != "" {
		offenders = append(offenders, "providerkeys.gemini")
	}
	if pp := strings.TrimSpace(cfg.PreferredProvider); pp != "" && pp != "openai" {
		offenders = append(offenders, "preferredprovider")
	}

	if len(offenders) == 0 {
		return nil
	}
	return fmt.Errorf(
		"invalid configuration: only the OpenAI provider is currently supported; remove/clear %s",
		strings.Join(offenders, ", "),
	)
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
