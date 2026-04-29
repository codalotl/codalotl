package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/q/cli"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveAddDocsConfig_Defaults(t *testing.T) {
	cfg, err := resolveAddDocsConfig(addDocsFlagValues{})

	require.NoError(t, err)
	assert.Equal(t, llmmodel.DefaultModel, cfg.model)
	assert.Equal(t, 0, cfg.reflowWidth)
	assert.Empty(t, cfg.logFile)
	assert.False(t, cfg.documentTestFiles)
	assert.False(t, cfg.onlyPublicAPI)
	assert.Nil(t, cfg.excludeIdentifiers)
	assert.Equal(t, 0, cfg.tokenBudget)
}

func TestResolveAddDocsConfig_Flags(t *testing.T) {
	cfg, err := resolveAddDocsConfig(addDocsFlagValues{
		model:              string(llmmodel.DefaultModel),
		reflowWidth:        80,
		logFile:            "flags.log",
		documentTestFiles:  true,
		onlyPublicAPI:      true,
		excludeIdentifiers: "Foo, Bar,,Baz",
		tokenBudget:        100,
	})

	require.NoError(t, err)
	assert.Equal(t, 80, cfg.reflowWidth)
	assert.Equal(t, "flags.log", cfg.logFile)
	assert.True(t, cfg.documentTestFiles)
	assert.True(t, cfg.onlyPublicAPI)
	assert.Equal(t, []string{"Foo", "Bar", "Baz"}, cfg.excludeIdentifiers)
	assert.Equal(t, 100, cfg.tokenBudget)
}

func TestResolveAddDocsConfig_InvalidModel(t *testing.T) {
	_, err := resolveAddDocsConfig(addDocsFlagValues{
		model: "not-a-model",
	})

	require.Error(t, err)
	assert.ErrorContains(t, err, "invalid --model")
}

func TestRunDocRequiresPackageArg(t *testing.T) {
	root := newRootCommand()
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := cli.Run(context.Background(), root, cli.Options{
		Args: []string{"doc"},
		Out:  &stdout,
		Err:  &stderr,
	})

	assert.Equal(t, 2, code)
	assert.Empty(t, stdout.String())
	assert.Contains(t, stderr.String(), "Usage:")
}

func TestRunDocRejectsInvalidTokenBudgetBeforeLoadingPackage(t *testing.T) {
	root := newRootCommand()
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := cli.Run(context.Background(), root, cli.Options{
		Args: []string{"doc", ".", "--token-budget=-1"},
		Out:  &stdout,
		Err:  &stderr,
	})

	assert.Equal(t, 2, code)
	assert.Empty(t, stdout.String())
	assert.Contains(t, stderr.String(), "--token-budget must be >= 0")
}

func TestRunDocSendsDocubotProgressToCLIWriter(t *testing.T) {
	pkgDir := writeDocumentedPackage(t)
	root := newRootCommand()
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	var code int
	processStdout := captureStdout(t, func() {
		code = cli.Run(context.Background(), root, cli.Options{
			Args: []string{"doc", pkgDir},
			Out:  &stdout,
			Err:  &stderr,
		})
	})

	assert.Equal(t, 0, code)
	assert.Empty(t, stderr.String())
	assert.Contains(t, stdout.String(), "Everything is already documented")
	assert.Contains(t, stdout.String(), "Applied 0 documentation change(s).")
	assert.Empty(t, processStdout)
}

func writeDocumentedPackage(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/docubotcmdtest\n\ngo 1.22\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "example.go"), []byte("// Package example is a documented package used by docubot cmd tests.\npackage example\n\n// Answer returns the answer.\nfunc Answer() int { return 42 }\n"), 0644))
	return dir
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	originalStdout := os.Stdout
	reader, writer, err := os.Pipe()
	require.NoError(t, err)

	os.Stdout = writer
	defer func() {
		os.Stdout = originalStdout
	}()

	fn()

	require.NoError(t, writer.Close())
	output, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	return string(output)
}
