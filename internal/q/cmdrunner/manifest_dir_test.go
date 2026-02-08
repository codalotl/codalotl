package cmdrunner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestManifestDirWithLangInput(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	projectDir := filepath.Join(root, "project")
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "pkg"), 0o755))
	writeFile(t, filepath.Join(projectDir, "pyproject.toml"), "")

	resolver := newManifestDirResolver(root, map[string]any{"Lang": "py"})
	got, err := resolver.manifestDir(filepath.Join(projectDir, "pkg"))
	require.NoError(t, err)
	require.Equal(t, projectDir, got)
}

func TestManifestDirDetectsLanguageFromFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	moduleDir := filepath.Join(root, "module")
	serverDir := filepath.Join(moduleDir, "cmd", "server")
	require.NoError(t, os.MkdirAll(serverDir, 0o755))

	writeFile(t, filepath.Join(moduleDir, "go.mod"), "module example.com/module\n")
	writeFile(t, filepath.Join(serverDir, "main.go"), "package main\n")

	resolver := newManifestDirResolver(root, nil)
	got, err := resolver.manifestDir(filepath.Join(serverDir, "main.go"))
	require.NoError(t, err)
	require.Equal(t, moduleDir, got)
}

func TestManifestDirWalksUpWhenDirHasNoFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	projectDir := filepath.Join(root, "project")
	innerDir := filepath.Join(projectDir, "inner", "deep")

	require.NoError(t, os.MkdirAll(innerDir, 0o755))
	writeFile(t, filepath.Join(projectDir, "main.py"), "print('hello')\n")
	writeFile(t, filepath.Join(projectDir, "pyproject.toml"), "")

	resolver := newManifestDirResolver(root, nil)
	got, err := resolver.manifestDir(innerDir)
	require.NoError(t, err)
	require.Equal(t, projectDir, got)
}

func TestManifestDirReturnsRootWhenUnknownLanguage(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	docsDir := filepath.Join(root, "docs")
	require.NoError(t, os.MkdirAll(docsDir, 0o755))
	writeFile(t, filepath.Join(docsDir, "README"), "some docs\n")

	resolver := newManifestDirResolver(root, nil)
	got, err := resolver.manifestDir(docsDir)
	require.NoError(t, err)
	require.Equal(t, root, got)
}

func TestManifestDirReturnsRootWhenManifestMissing(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	goDir := filepath.Join(root, "go")
	require.NoError(t, os.MkdirAll(goDir, 0o755))
	writeFile(t, filepath.Join(goDir, "main.go"), "package main\n")

	resolver := newManifestDirResolver(root, map[string]any{"Lang": "go"})
	got, err := resolver.manifestDir(goDir)
	require.NoError(t, err)
	require.Equal(t, root, got)
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o644))
}

func TestManifestDirAPI(t *testing.T) {
	t.Parallel()

	type setupResult struct {
		root          string
		pathArg       string
		wantManifest  string
		wantRelative  string
		wantErrSubstr string
	}

	tests := []struct {
		name  string
		setup func(t *testing.T) setupResult
	}{
		{
			name: "go file detects module manifest",
			setup: func(t *testing.T) setupResult {
				t.Helper()

				root := t.TempDir()
				moduleDir := filepath.Join(root, "module")
				serverDir := filepath.Join(moduleDir, "cmd", "server")
				require.NoError(t, os.MkdirAll(serverDir, 0o755))

				writeFile(t, filepath.Join(moduleDir, "go.mod"), "module example.com/module\n")
				writeFile(t, filepath.Join(serverDir, "main.go"), "package main\n")

				return setupResult{
					root:         root,
					pathArg:      filepath.Join("module", "cmd", "server", "main.go"),
					wantManifest: moduleDir,
					wantRelative: filepath.Join("cmd", "server", "main.go"),
				}
			},
		},
		{
			name: "empty dir walks up to find language then manifest",
			setup: func(t *testing.T) setupResult {
				t.Helper()

				root := t.TempDir()
				projectDir := filepath.Join(root, "project")
				innerDir := filepath.Join(projectDir, "inner", "deep")
				require.NoError(t, os.MkdirAll(innerDir, 0o755))

				writeFile(t, filepath.Join(projectDir, "main.py"), "print('hello')\n")
				writeFile(t, filepath.Join(projectDir, "pyproject.toml"), "")

				return setupResult{
					root:         root,
					pathArg:      filepath.Join("project", "inner", "deep"),
					wantManifest: projectDir,
					wantRelative: filepath.Join("inner", "deep"),
				}
			},
		},
		{
			name: "unknown language falls back to root",
			setup: func(t *testing.T) setupResult {
				t.Helper()

				root := t.TempDir()
				docsDir := filepath.Join(root, "docs")
				require.NoError(t, os.MkdirAll(docsDir, 0o755))
				writeFile(t, filepath.Join(docsDir, "README"), "some docs\n")

				return setupResult{
					root:         root,
					pathArg:      "docs",
					wantManifest: filepath.Clean(root),
					wantRelative: "docs",
				}
			},
		},
		{
			name: "manifest missing falls back to root even if language detected",
			setup: func(t *testing.T) setupResult {
				t.Helper()

				root := t.TempDir()
				goDir := filepath.Join(root, "go")
				require.NoError(t, os.MkdirAll(goDir, 0o755))
				writeFile(t, filepath.Join(goDir, "main.go"), "package main\n")

				return setupResult{
					root:         root,
					pathArg:      "go",
					wantManifest: filepath.Clean(root),
					wantRelative: "go",
				}
			},
		},
		{
			name: "path empty treats it as root",
			setup: func(t *testing.T) setupResult {
				t.Helper()

				root := t.TempDir()
				return setupResult{
					root:         root,
					pathArg:      "",
					wantManifest: filepath.Clean(root),
					wantRelative: ".",
				}
			},
		},
		{
			name: "absolute path is accepted",
			setup: func(t *testing.T) setupResult {
				t.Helper()

				root := t.TempDir()
				moduleDir := filepath.Join(root, "module")
				pkgDir := filepath.Join(moduleDir, "pkg")
				require.NoError(t, os.MkdirAll(pkgDir, 0o755))
				writeFile(t, filepath.Join(moduleDir, "go.mod"), "module example.com/module\n")
				writeFile(t, filepath.Join(pkgDir, "main.go"), "package pkg\n")

				return setupResult{
					root:         root,
					pathArg:      filepath.Join(pkgDir, "main.go"),
					wantManifest: moduleDir,
					wantRelative: filepath.Join("pkg", "main.go"),
				}
			},
		},
		{
			name: "errors when rootDir is empty",
			setup: func(t *testing.T) setupResult {
				t.Helper()
				return setupResult{
					root:          "",
					pathArg:       "anything",
					wantErrSubstr: "rootDir must not be empty",
				}
			},
		},
		{
			name: "errors when rootDir does not exist",
			setup: func(t *testing.T) setupResult {
				t.Helper()

				root := filepath.Join(t.TempDir(), "does-not-exist")
				return setupResult{
					root:          root,
					pathArg:       "anything",
					wantErrSubstr: "rootDir",
				}
			},
		},
		{
			name: "errors when rootDir is a file",
			setup: func(t *testing.T) setupResult {
				t.Helper()

				dir := t.TempDir()
				fileRoot := filepath.Join(dir, "root.txt")
				require.NoError(t, os.WriteFile(fileRoot, []byte("data"), 0o644))
				return setupResult{
					root:          fileRoot,
					pathArg:       "anything",
					wantErrSubstr: "not a directory",
				}
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s := tc.setup(t)
			gotManifest, gotRelative, err := ManifestDir(s.root, s.pathArg)

			if s.wantErrSubstr != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, s.wantErrSubstr)
				return
			}

			require.NoError(t, err)
			require.Equal(t, s.wantManifest, gotManifest)
			require.Equal(t, s.wantRelative, gotRelative)
		})
	}
}
