package main

import (
	"bytes"
	"context"
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
	assert.Nil(t, cfg.excludeIdentifiers)
	assert.Equal(t, 0, cfg.tokenBudget)
}

func TestResolveAddDocsConfig_Flags(t *testing.T) {
	cfg, err := resolveAddDocsConfig(addDocsFlagValues{
		model:              string(llmmodel.DefaultModel),
		reflowWidth:        80,
		logFile:            "flags.log",
		documentTestFiles:  true,
		excludeIdentifiers: "Foo, Bar,,Baz",
		tokenBudget:        100,
	})

	require.NoError(t, err)
	assert.Equal(t, 80, cfg.reflowWidth)
	assert.Equal(t, "flags.log", cfg.logFile)
	assert.True(t, cfg.documentTestFiles)
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
