package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/stretchr/testify/require"
)

func sanitizeTestModelID(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z':
			b.WriteByte(c)
		case c >= '0' && c <= '9':
			b.WriteByte(c)
		case c == '-' || c == '.':
			// Keep a small set of common separators; llmmodel's validation may be
			// stricter, but these are typical.
			b.WriteByte(c)
		case c == '_':
			b.WriteByte('-')
		default:
			b.WriteByte('-')
		}
	}
	out := b.String()
	out = strings.Trim(out, "-")
	if out == "" {
		return "custom-test-model"
	}
	return out
}

func TestLoadConfig_CustomModels_RegistersAndIsIdempotent(t *testing.T) {
	isolateUserConfig(t)

	customID := "custom-" + sanitizeTestModelID(t.Name())
	keyEnv := "CODALOTL_TEST_CUSTOM_MODEL_KEY_" + strings.ToUpper(strings.ReplaceAll(t.Name(), "Test", ""))
	endpointEnv := "CODALOTL_TEST_CUSTOM_ENDPOINT_" + strings.ToUpper(strings.ReplaceAll(t.Name(), "Test", ""))

	t.Setenv(keyEnv, "sk-custom")
	t.Setenv(endpointEnv, "https://example.test/v1")

	tmp := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, ".codalotl"), 0755))
	cfgPath := filepath.Join(tmp, ".codalotl", "config.json")
	cfgJSON := `{
  "custommodels": [
    {
      "id": "` + customID + `",
      "provider": "openai",
      "model": "gpt-custom",
      "apikeyenv": "` + keyEnv + `",
      "apiendpointenv": "` + endpointEnv + `",
      "apiendpointurl": "https://ignored.example/v1",
      "reasoningeffort": "medium",
      "servicetier": "priority"
    }
  ],
  "preferredmodel": "` + customID + `"
}
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfgJSON), 0644))

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	cfg, err := loadConfig()
	require.NoError(t, err)

	require.Equal(t, llmmodel.ModelID(customID), effectiveModel(cfg))
	require.True(t, llmmodel.ModelID(customID).Valid())
	require.Equal(t, "sk-custom", llmmodel.GetAPIKey(llmmodel.ModelID(customID)))
	require.Equal(t, "https://example.test/v1", llmmodel.GetAPIEndpointURL(llmmodel.ModelID(customID)))

	// Ensure repeated loads don't fail due to llmmodel's process-global registry.
	cfg2, err := loadConfig()
	require.NoError(t, err)
	require.Equal(t, llmmodel.ModelID(customID), effectiveModel(cfg2))
}

func TestRun_Config_CustomModelKeySatisfiesStartupValidation(t *testing.T) {
	isolateUserConfig(t)

	// Ensure the built-in provider key isn't the thing satisfying startup validation.
	t.Setenv("OPENAI_API_KEY", "")

	customID := "custom-" + sanitizeTestModelID(t.Name())
	keyEnv := "CODALOTL_TEST_CUSTOM_ONLY_KEY_" + strings.ToUpper(strings.ReplaceAll(t.Name(), "Test", ""))
	t.Setenv(keyEnv, "sk-custom-only")

	tmp := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, ".codalotl"), 0755))
	cfgPath := filepath.Join(tmp, ".codalotl", "config.json")
	cfgJSON := `{
  "custommodels": [
    {
      "id": "` + customID + `",
      "provider": "openai",
      "model": "gpt-custom",
      "apikeyenv": "` + keyEnv + `"
    }
  ]
}
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfgJSON), 0644))

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "config"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())

	require.Contains(t, out.String(), keyEnv)

	// The JSON block should include custommodels (and preserve the entry).
	cfgBlock := extractConfigJSON(t, out.String())
	var got map[string]any
	require.NoError(t, json.Unmarshal([]byte(cfgBlock), &got))
	require.Contains(t, got, "custommodels")
}
