package coretools

import (
	"github.com/codalotl/codalotl/internal/gocodetesting"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var dedent = gocodetesting.Dedent

func TestNormalizePath(t *testing.T) {
	sandbox := t.TempDir()

	innerDir := filepath.Join(sandbox, "inner")
	require.NoError(t, os.Mkdir(innerDir, 0o755))

	innerFile := filepath.Join(innerDir, "file.txt")
	require.NoError(t, os.WriteFile(innerFile, []byte("content"), 0o644))

	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "outside.txt")
	require.NoError(t, os.WriteFile(outsideFile, []byte("outside"), 0o644))

	tests := []struct {
		name          string
		path          string
		want          WantPathType
		mustExist     bool
		expectAbs     string
		expectRel     string
		errorContains string
		errorIs       error
	}{
		{
			name:      "relative_file_inside_sandbox",
			path:      filepath.Join("inner", "file.txt"),
			want:      WantPathTypeAny,
			mustExist: true,
			expectAbs: innerFile,
			expectRel: filepath.Join("inner", "file.txt"),
		},
		{
			name:      "coerce_file_to_dir",
			path:      filepath.Join("inner", "file.txt"),
			want:      WantPathTypeDir,
			mustExist: true,
			expectAbs: innerDir,
			expectRel: filepath.Join("inner"),
		},
		{
			name:          "directory_when_want_file",
			path:          filepath.Join("inner"),
			want:          WantPathTypeFile,
			mustExist:     true,
			errorContains: "directory",
		},
		{
			name:      "absolute_path_outside_sandbox",
			path:      outsideFile,
			want:      WantPathTypeAny,
			mustExist: true,
			expectAbs: outsideFile,
			expectRel: "",
		},
		{
			name:      "nonexistent_path_no_coercion",
			path:      "newfile.txt",
			want:      WantPathTypeDir,
			mustExist: false,
			expectAbs: filepath.Join(sandbox, "newfile.txt"),
			expectRel: "newfile.txt",
		},
		{
			name:      "must_exist_missing_path",
			path:      filepath.Join("missing", "file.txt"),
			want:      WantPathTypeAny,
			mustExist: true,
			errorIs:   os.ErrNotExist,
		},
		{
			name:      "path is sandbox",
			path:      sandbox,
			want:      WantPathTypeAny,
			mustExist: true,
			expectAbs: sandbox,
			expectRel: ".",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			abs, rel, err := NormalizePath(tt.path, sandbox, tt.want, tt.mustExist)
			if tt.errorContains != "" || tt.errorIs != nil {
				require.Error(t, err)
				if tt.errorContains != "" {
					require.ErrorContains(t, err, tt.errorContains)
				}
				if tt.errorIs != nil {
					assert.ErrorIs(t, err, tt.errorIs)
				}
				assert.Empty(t, abs)
				assert.Empty(t, rel)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectAbs, abs)
			assert.Equal(t, tt.expectRel, rel)
		})
	}
}
