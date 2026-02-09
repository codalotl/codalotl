package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRun_Config_ParsesLintsConfig(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, ".codalotl"), 0755))
	cfgPath := filepath.Join(tmp, ".codalotl", "config.json")
	cfg := `{
  "providerkeys": { "openai": "sk-test" },
  "lints": {
    "mode": "extend",
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
}
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfg), 0644))

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

	cfgJSON := extractConfigJSON(t, out.String())
	require.Contains(t, cfgJSON, `"lints"`)
	require.Contains(t, cfgJSON, `"staticcheck"`)
}

func TestRun_ContextInitial_UsesLintsFromConfig(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()

	// Create a tiny module with one package.
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "p"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "p", "p.go"), []byte("package p\n\nfunc P() {}\n"), 0644))

	// Configure lints to *replace* defaults with:
	//   - gofmt (a non-reflow step that should run in initial context), and
	//   - reflow (which must NOT run in initial context; see internal/lints/SPEC.md).
	//
	// Set a non-default width so we can prove config is wired into ResolveSteps
	// (reflow steps should still be normalized, even if skipped in initial context).
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, ".codalotl"), 0755))
	cfgPath := filepath.Join(tmp, ".codalotl", "config.json")
	cfg := `{
  "providerkeys": { "openai": "sk-test" },
  "reflowwidth": 77,
  "lints": {
    "mode": "replace",
    "steps": [
      {
        "id": "gofmt",
        "check": {
          "command": "gofmt",
          "args": ["-l", "{{ .relativePackageDir }}"],
          "cwd": "{{ .moduleDir }}"
        }
      },
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
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfg), 0644))

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "context", "initial", "./p"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())

	got := out.String()
	require.Contains(t, got, "<lint-status")
	require.Contains(t, got, "gofmt")
	require.NotContains(t, got, "codalotl docs reflow")
}
