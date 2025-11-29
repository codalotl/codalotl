package cascade

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProvidence_Scalar_DefaultJsonEnv(t *testing.T) {
	type C struct {
		Port           int
		PortProvidence Providence `json:"-"`
		Name           string
		NameProvidence Providence
	}

	withJSON(t, "cfg.json", `{"port": 8080, "name": "fromjson"}`, func(p string) {
		withEnv(t, map[string]string{"ENV_PORT": "9090"}, func() {
			var cfg C
			err := New().
				WithDefaults(map[string]any{"port": 80, "name": "def"}).
				WithJSONFile(p).
				WithEnv(map[string]string{"port": "ENV_PORT"}).
				StrictlyLoad(&cfg)
			require.NoError(t, err)

			assert.Equal(t, 9090, cfg.Port)
			assert.Equal(t, "env", cfg.PortProvidence.SourceType)
			assert.Equal(t, "", cfg.PortProvidence.SourceIdentifier)

			assert.Equal(t, "fromjson", cfg.Name)
			assert.Equal(t, "json_file", cfg.NameProvidence.SourceType)
			assert.Equal(t, false, cfg.NameProvidence.Default())
			assert.Equal(t, true, cfg.NameProvidence.IsSet())
			// Path recorded should be absolute (expanded)
			assert.Equal(t, ExpandPath(p), cfg.NameProvidence.SourceIdentifier)
		})
	})
}

func TestProvidence_DefaultOnly(t *testing.T) {
	type C struct {
		TimeoutSecs           int
		TimeoutSecsProvidence Providence
	}

	var cfg C
	err := New().WithDefaults(map[string]any{"timeoutsecs": 30}).StrictlyLoad(&cfg)
	require.NoError(t, err)
	assert.Equal(t, 30, cfg.TimeoutSecs)
	assert.Equal(t, "default", cfg.TimeoutSecsProvidence.SourceType)
	assert.Equal(t, true, cfg.TimeoutSecsProvidence.Default())
	assert.Equal(t, true, cfg.TimeoutSecsProvidence.IsSet())
	assert.Equal(t, "", cfg.TimeoutSecsProvidence.SourceIdentifier)
}

func TestProvidence_NestedStruct(t *testing.T) {
	type Server struct {
		Host           string
		HostProvidence Providence
		Port           int
		PortProvidence Providence
	}
	type C struct {
		Server Server
	}

	withJSON(t, "srv.json", `{"server": {"host": "fromjson"}}`, func(p string) {
		withEnv(t, map[string]string{"ENV_PORT": "8081"}, func() {
			var cfg C
			err := New().
				WithDefaults(map[string]any{"server.port": 8080}).
				WithJSONFile(p).
				WithEnv(map[string]string{"server.port": "ENV_PORT"}).
				StrictlyLoad(&cfg)
			require.NoError(t, err)

			assert.Equal(t, "fromjson", cfg.Server.Host)
			assert.Equal(t, "json_file", cfg.Server.HostProvidence.SourceType)
			assert.Equal(t, ExpandPath(p), cfg.Server.HostProvidence.SourceIdentifier)

			assert.Equal(t, 8081, cfg.Server.Port)
			assert.Equal(t, "env", cfg.Server.PortProvidence.SourceType)
			assert.Equal(t, "", cfg.Server.PortProvidence.SourceIdentifier)
		})
	})
}

func TestProvidence_SliceAssignment(t *testing.T) {
	type C struct {
		Tags           []string
		TagsProvidence Providence
	}

	withJSON(t, "tags.json", `{"tags": ["a", "b"]}`, func(p string) {
		var cfg C
		err := New().WithJSONFile(p).StrictlyLoad(&cfg)
		require.NoError(t, err)
		assert.Equal(t, []string{"a", "b"}, cfg.Tags)
		assert.Equal(t, "json_file", cfg.TagsProvidence.SourceType)
		assert.Equal(t, ExpandPath(p), cfg.TagsProvidence.SourceIdentifier)
	})
}

func TestProvidence_Unset(t *testing.T) {
	type C struct {
		Val           string
		ValProvidence Providence
	}

	withJSON(t, "config.json", `{}`, func(p string) {
		var cfg C
		err := New().WithJSONFile(p).StrictlyLoad(&cfg)
		require.NoError(t, err)
		assert.Equal(t, false, cfg.ValProvidence.IsSet())
	})
}
