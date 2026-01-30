package cascade

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func jsonFilePaths(r LoadReport) []string {
	var out []string
	for _, p := range r.Sources {
		if p.SourceType == "json_file" {
			out = append(out, p.SourceIdentifier)
		}
	}
	return out
}

func TestStrictlyLoadWithReport_JSONFiles(t *testing.T) {
	type C struct {
		Name string
	}

	t.Run("if neither file exists, report has no json_file entries", func(t *testing.T) {
		base := t.TempDir()
		start := filepath.Join(base, "workdir", "child")
		require.NoError(t, os.MkdirAll(start, 0o755))

		global := filepath.Join(base, "global.json") // not created

		var cfg C
		r, err := New().
			WithJSONFile(global).
			WithNearestJSONFile("local.json", start).
			StrictlyLoadWithReport(&cfg)
		require.NoError(t, err)

		assert.Empty(t, jsonFilePaths(r))
	})

	t.Run("if global exists, report contains it", func(t *testing.T) {
		base := t.TempDir()
		global := filepath.Join(base, "global.json")
		require.NoError(t, os.WriteFile(global, []byte(`{"name":"global"}`), 0o644))

		var cfg C
		r, err := New().WithJSONFile(global).StrictlyLoadWithReport(&cfg)
		require.NoError(t, err)
		assert.Equal(t, "global", cfg.Name)

		assert.Equal(t, []string{ExpandPath(global)}, jsonFilePaths(r))
	})

	t.Run("if local nearest exists, report contains it", func(t *testing.T) {
		base := t.TempDir()
		parent := filepath.Join(base, "p")
		child := filepath.Join(parent, "c")
		require.NoError(t, os.MkdirAll(child, 0o755))

		local := filepath.Join(child, "config.json")
		require.NoError(t, os.WriteFile(local, []byte(`{"name":"local"}`), 0o644))

		var cfg C
		r, err := New().WithNearestJSONFile("config.json", child).StrictlyLoadWithReport(&cfg)
		require.NoError(t, err)
		assert.Equal(t, "local", cfg.Name)

		assert.Equal(t, []string{ExpandPath(local)}, jsonFilePaths(r))
	})

	t.Run("if both exist, report lists both in precedence order", func(t *testing.T) {
		base := t.TempDir()
		parent := filepath.Join(base, "p")
		child := filepath.Join(parent, "c")
		require.NoError(t, os.MkdirAll(child, 0o755))

		global := filepath.Join(base, "global.json")
		require.NoError(t, os.WriteFile(global, []byte(`{"name":"global"}`), 0o644))

		local := filepath.Join(child, "config.json")
		require.NoError(t, os.WriteFile(local, []byte(`{"name":"local"}`), 0o644))

		var cfg C
		r, err := New().
			WithJSONFile(global).
			WithNearestJSONFile("config.json", child).
			StrictlyLoadWithReport(&cfg)
		require.NoError(t, err)
		assert.Equal(t, "local", cfg.Name)

		assert.Equal(t, []string{ExpandPath(global), ExpandPath(local)}, jsonFilePaths(r))
	})
}
