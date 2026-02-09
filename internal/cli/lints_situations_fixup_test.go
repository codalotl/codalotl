package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codalotl/codalotl/internal/lints"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig_LintsStepSituations_OmittedRemainsNil(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, ".codalotl"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, ".codalotl", "config.json"), []byte(`{
  "providerkeys": { "openai": "sk-test" },
  "lints": {
    "mode": "replace",
    "steps": [
      {
        "id": "reflow",
        "check": {
          "command": "codalotl",
          "args": ["docs", "reflow", "{{ .relativePackageDir }}"],
          "cwd": "{{ .moduleDir }}"
        }
      }
    ]
  }
}
`), 0644))

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	cfg, err := loadConfig()
	require.NoError(t, err)
	require.Len(t, cfg.Lints.Steps, 1)
	require.Nil(t, cfg.Lints.Steps[0].Situations)

	steps, err := lints.ResolveSteps(&cfg.Lints, cfg.ReflowWidth)
	require.NoError(t, err)
	require.Len(t, steps, 1)
	require.Nil(t, steps[0].Situations)
	require.NotNil(t, steps[0].Check)
}

func TestLoadConfig_LintsStepSituations_ExplicitEmptyPreserved(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, ".codalotl"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, ".codalotl", "config.json"), []byte(`{
  "providerkeys": { "openai": "sk-test" },
  "lints": {
    "mode": "replace",
    "steps": [
      {
        "id": "reflow",
        "situations": [],
        "check": {
          "command": "codalotl",
          "args": ["docs", "reflow", "{{ .relativePackageDir }}"],
          "cwd": "{{ .moduleDir }}"
        }
      }
    ]
  }
}
`), 0644))

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	cfg, err := loadConfig()
	require.NoError(t, err)
	require.Len(t, cfg.Lints.Steps, 1)
	require.NotNil(t, cfg.Lints.Steps[0].Situations)
	require.Len(t, cfg.Lints.Steps[0].Situations, 0)
}
