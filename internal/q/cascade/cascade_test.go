package cascade

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCascadeBasics(t *testing.T) {
	type Config struct {
		SpecialName string `json:"name"`
		Port        int
		Debug       bool
		GasRatio    float64 `cascade:"ratio"`
		Tags        []string
		Thresholds  []int
		Rates       []float64
		TimeoutSecs int
	}

	// Create a JSON file with mid-priority values (overrides defaults, can be overridden by env):
	withJSON(t, "config.json", `{
        "name": "fromjson",
        "port": 8080,
        "debug": true,
        "ratio": 2.5,
        "tags": ["json1", "json2"],
        "thresholds": [3, 4],
        "rates": [3.3, 4.4]
    }`, func(jsonPath string) {
		// Highest-priority overrides via ENV (strings will be coerced as needed):
		withEnv(t, map[string]string{
			"ENV_NAME":  "fromenv",
			"ENV_PORT":  "9090",
			"ENV_DEBUG": "false",
			"ENV_RATIO": "3.14",
		}, func() {
			cfg := Config{}

			err := New().
				WithDefaults(map[string]any{
					"name":        "default",
					"port":        80,
					"debug":       false,
					"ratio":       1.5,
					"tags":        []string{"a", "b"},
					"thresholds":  []int{1, 2},
					"rates":       []float64{0.1, 0.2},
					"timeoutsecs": 30,
				}).
				WithJSONFile(jsonPath).
				WithEnv(map[string]string{
					"name":  "ENV_NAME",
					"port":  "ENV_PORT",
					"debug": "ENV_DEBUG",
					"ratio": "ENV_RATIO",
				}).
				StrictlyLoad(&cfg)

			require.NoError(t, err)

			// ENV overrides JSON and defaults:
			assert.Equal(t, "fromenv", cfg.SpecialName)
			assert.Equal(t, 9090, cfg.Port)
			assert.Equal(t, false, cfg.Debug)
			assert.InDelta(t, 3.14, cfg.GasRatio, 1e-9)

			// Slices come from JSON when not overridden by ENV:
			assert.Equal(t, []string{"json1", "json2"}, cfg.Tags)
			assert.Equal(t, []int{3, 4}, cfg.Thresholds)
			assert.InDeltaSlice(t, []float64{3.3, 4.4}, cfg.Rates, 1e-9)

			// Default-only value remains when not present in higher sources:
			assert.Equal(t, 30, cfg.TimeoutSecs)
		})
	})
}

func TestCascade_ObjectSlice(t *testing.T) {
	type Config struct {
		Name    string
		Servers []struct {
			Host string `cascade:",required"`
			Port string `cascade:",required"`
		}
	}

	t.Run("works with a few servers", func(t *testing.T) {
		withJSON(t, "servers.json", `{
			"servers": [
				{"host": "a.example.com", "port": "8080"},
				{"host": "b.example.com", "port": "9090"}
			]
		}`, func(p string) {
			var cfg Config
			err := New().WithJSONFile(p).StrictlyLoad(&cfg)
			require.NoError(t, err)
			require.Len(t, cfg.Servers, 2)
			assert.Equal(t, "a.example.com", cfg.Servers[0].Host)
			assert.Equal(t, "8080", cfg.Servers[0].Port)
			assert.Equal(t, "b.example.com", cfg.Servers[1].Host)
			assert.Equal(t, "9090", cfg.Servers[1].Port)
		})
	})

	t.Run("empty servers array is allowed", func(t *testing.T) {
		withJSON(t, "empty.json", `{
			"servers": []
		}`, func(p string) {
			var cfg Config
			err := New().WithJSONFile(p).StrictlyLoad(&cfg)
			require.NoError(t, err)
			assert.Len(t, cfg.Servers, 0)
		})
	})

	t.Run("error when an element is missing a required field", func(t *testing.T) {
		withJSON(t, "bad.json", `{
			"servers": [
				{"host": "a.example.com"},
				{"host": "b.example.com", "port": "9090"}
			]
		}`, func(p string) {
			var cfg Config
			err := New().WithJSONFile(p).StrictlyLoad(&cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "missing required key: servers[0].port")
		})
	})
}

func TestCascade_ObjectSlice_NestedStructAndPointer(t *testing.T) {
	type Command struct {
		Command string
		Args    []string
		CWD     string
	}

	t.Run("slice element contains nested struct value", func(t *testing.T) {
		type Step struct {
			ID    string
			Check Command
		}
		type Config struct {
			Lints struct {
				Steps []Step
			}
		}

		withJSON(t, "lints.json", `{
			"lints": {
				"steps": [
					{
						"id": "staticcheck",
						"check": {
							"command": "staticcheck",
							"args": ["{{ .relativePackageDir }}"],
							"cwd": "{{ .moduleDir }}"
						}
					}
				]
			}
		}`, func(p string) {
			var cfg Config
			err := New().WithJSONFile(p).StrictlyLoad(&cfg)
			require.NoError(t, err)
			require.Len(t, cfg.Lints.Steps, 1)
			assert.Equal(t, "staticcheck", cfg.Lints.Steps[0].ID)
			assert.Equal(t, "staticcheck", cfg.Lints.Steps[0].Check.Command)
			assert.Equal(t, []string{"{{ .relativePackageDir }}"}, cfg.Lints.Steps[0].Check.Args)
			assert.Equal(t, "{{ .moduleDir }}", cfg.Lints.Steps[0].Check.CWD)
		})
	})

	t.Run("slice element contains nested *struct value (pointer allocated)", func(t *testing.T) {
		type Step struct {
			ID    string
			Check *Command
		}
		type Config struct {
			Lints struct {
				Steps []Step
			}
		}

		withJSON(t, "lints.json", `{
			"lints": {
				"steps": [
					{
						"id": "staticcheck",
						"check": {
							"command": "staticcheck",
							"args": ["{{ .relativePackageDir }}"],
							"cwd": "{{ .moduleDir }}"
						}
					}
				]
			}
		}`, func(p string) {
			var cfg Config
			err := New().WithJSONFile(p).StrictlyLoad(&cfg)
			require.NoError(t, err)
			require.Len(t, cfg.Lints.Steps, 1)
			assert.Equal(t, "staticcheck", cfg.Lints.Steps[0].ID)
			require.NotNil(t, cfg.Lints.Steps[0].Check)
			assert.Equal(t, "staticcheck", cfg.Lints.Steps[0].Check.Command)
			assert.Equal(t, []string{"{{ .relativePackageDir }}"}, cfg.Lints.Steps[0].Check.Args)
			assert.Equal(t, "{{ .moduleDir }}", cfg.Lints.Steps[0].Check.CWD)
		})
	})
}

func TestCascade_StrictlyLoadCases(t *testing.T) {
	t.Run("file not found does not error", func(t *testing.T) {
		type C struct{ Port int }
		var cfg C
		// Nonexistent path
		err := New().WithJSONFile("/definitely/does/not/exist-12345.json").StrictlyLoad(&cfg)
		require.NoError(t, err)
	})

	t.Run("permission error does not error", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("chmod(000) does not revoke read perms on Windows")
		}

		type C struct{ Name string }
		withJSON(t, "priv.json", `{"name":"x"}`, func(p string) {
			// Remove read permission so open fails with EPERM
			require.NoError(t, os.Chmod(p, 0o000))
			defer func() { _ = os.Chmod(p, 0o644) }()
			if _, err := os.ReadFile(p); err == nil {
				t.Skip("environment permits reading files without permissions; skipping permission error test")
			}
			var cfg C
			err := New().WithJSONFile(p).StrictlyLoad(&cfg)
			require.NoError(t, err)
			require.Equal(t, "", cfg.Name)
		})
	})

	t.Run("empty file does not error", func(t *testing.T) {
		type C struct{ Debug bool }
		withJSON(t, "empty.json", "   \n\t  ", func(p string) {
			var cfg C
			err := New().WithJSONFile(p).StrictlyLoad(&cfg)
			require.NoError(t, err)
			assert.Equal(t, false, cfg.Debug)
		})
	})

	t.Run("extraneous keys are ignored", func(t *testing.T) {
		type C struct{ Port int }
		withJSON(t, "extra.json", `{"port": 8080, "unknown": 1}`, func(p string) {
			var cfg C
			err := New().WithJSONFile(p).StrictlyLoad(&cfg)
			require.NoError(t, err)
			assert.Equal(t, 8080, cfg.Port)
		})
	})

	t.Run("invalid destination cases", func(t *testing.T) {
		err := New().StrictlyLoad(nil)
		require.Error(t, err)
		err = New().StrictlyLoad(struct{}{})
		require.Error(t, err)
		var x int
		err = New().StrictlyLoad(&x)
		require.Error(t, err)
		// valid pointer to struct works
		var ok struct{}
		err = New().StrictlyLoad(&ok)
		require.NoError(t, err)
	})

	t.Run("parse error in readable source errors", func(t *testing.T) {
		type C struct{ Name string }
		withJSON(t, "bad.json", `{"name":`, func(p string) {
			var cfg C
			err := New().WithJSONFile(p).StrictlyLoad(&cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "parse json")
		})
	})

	t.Run("bad type in earlier source errors even if later good", func(t *testing.T) {
		type C struct{ Port int }
		withJSON(t, "badtype.json", `{"port": {"oops":1}}`, func(p string) {
			var cfg C
			err := New().
				WithDefaults(map[string]any{"port": 80}).
				WithJSONFile(p). // wrong type for port
				WithEnv(map[string]string{"port": "ENV_PORT"}).
				StrictlyLoad(&cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "cannot coerce")
		})
	})

	t.Run("missing required key errors after processing all sources", func(t *testing.T) {
		type C struct {
			Host string `cascade:",required"`
			Port int
		}
		var cfg C
		err := New().WithDefaults(map[string]any{"port": 1234}).StrictlyLoad(&cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing required key: host")
	})

	// When the default provides the zero-value for a required field, it still counts as provided
	t.Run("required field satisfied by zero-value default", func(t *testing.T) {
		type C struct {
			Host string `cascade:",required"`
			Port int
		}
		var cfg C
		err := New().WithDefaults(map[string]any{"host": "", "port": 0}).StrictlyLoad(&cfg)
		require.NoError(t, err)
		// All values should be present (no missing key error) and equal to defaults
		assert.Equal(t, "", cfg.Host)
		assert.Equal(t, 0, cfg.Port)
	})

	// Detect case-insensitive field collisions in destination struct
	t.Run("error on case-insensitive struct field collision", func(t *testing.T) {
		type C struct {
			URL string
			Url string
		}
		var cfg C
		// Ensure at least one source so that the loader attempts to apply into the struct
		err := New().WithDefaults(map[string]any{}).StrictlyLoad(&cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "case-insensitive field key collision")
	})
}

func TestWithNearestJSONFile(t *testing.T) {
	t.Run("picks nearest non-empty file", func(t *testing.T) {
		base := t.TempDir()
		parent := filepath.Join(base, "p")
		child := filepath.Join(parent, "c")
		require.NoError(t, os.MkdirAll(child, 0o755))

		parentCfg := filepath.Join(parent, "config.json")
		require.NoError(t, os.WriteFile(parentCfg, []byte(`{"name":"parent"}`), 0o644))
		childCfg := filepath.Join(child, "config.json")
		require.NoError(t, os.WriteFile(childCfg, []byte(`{"name":"child"}`), 0o644))

		type C struct{ Name string }
		var cfg C
		err := New().WithNearestJSONFile("config.json", child).StrictlyLoad(&cfg)
		require.NoError(t, err)
		assert.Equal(t, "child", cfg.Name)
	})

	t.Run("supports nested path in fileName", func(t *testing.T) {
		base := t.TempDir()
		parent := filepath.Join(base, "p")
		child := filepath.Join(parent, "c")
		require.NoError(t, os.MkdirAll(filepath.Join(child, "sub"), 0o755))
		require.NoError(t, os.MkdirAll(filepath.Join(parent, "sub"), 0o755))

		childApp := filepath.Join(child, "sub", "app.json")
		require.NoError(t, os.WriteFile(childApp, []byte(`{"port":1}`), 0o644))
		parentApp := filepath.Join(parent, "sub", "app.json")
		require.NoError(t, os.WriteFile(parentApp, []byte(`{"port":2}`), 0o644))

		type C struct{ Port int }
		var cfg C
		err := New().WithNearestJSONFile(filepath.Join("sub", "app.json"), child).StrictlyLoad(&cfg)
		require.NoError(t, err)
		assert.Equal(t, 1, cfg.Port)
	})

	t.Run("ignores empty nearest and uses parent", func(t *testing.T) {
		base := t.TempDir()
		parent := filepath.Join(base, "p")
		child := filepath.Join(parent, "c")
		require.NoError(t, os.MkdirAll(child, 0o755))

		childCfg := filepath.Join(child, "conf.json")
		require.NoError(t, os.WriteFile(childCfg, []byte("  \n\t  "), 0o644))
		parentCfg := filepath.Join(parent, "conf.json")
		require.NoError(t, os.WriteFile(parentCfg, []byte(`{"name":"parent"}`), 0o644))

		type C struct{ Name string }
		var cfg C
		err := New().WithNearestJSONFile("conf.json", child).StrictlyLoad(&cfg)
		require.NoError(t, err)
		assert.Equal(t, "parent", cfg.Name)
	})

	t.Run("no file found is fine", func(t *testing.T) {
		base := t.TempDir()
		start := filepath.Join(base, "x", "y")
		require.NoError(t, os.MkdirAll(start, 0o755))

		type C struct{ Name string }
		var cfg C
		err := New().
			WithDefaults(map[string]any{"name": "def"}).
			WithNearestJSONFile("missing.json", start).
			StrictlyLoad(&cfg)
		require.NoError(t, err)
		assert.Equal(t, "def", cfg.Name)
	})

	t.Run("panics when fileName is absolute", func(t *testing.T) {
		withJSON(t, "abs.json", `{"x":1}`, func(abs string) {
			start := t.TempDir()
			assert.Panics(t, func() {
				_ = New().WithNearestJSONFile(abs, start)
			})
		})
	})
}
