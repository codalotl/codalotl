package goclitools

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultRequiredTools(t *testing.T) {
	reqs := DefaultRequiredTools()
	require.Len(t, reqs, 5)
	assert.Equal(t, "go", reqs[0].Name)
	assert.Equal(t, "gopls", reqs[1].Name)
	assert.Equal(t, "goimports", reqs[2].Name)
	assert.Equal(t, "gofmt", reqs[3].Name)
	assert.Equal(t, "git", reqs[4].Name)

	assert.Empty(t, reqs[0].InstallHint)
	assert.Equal(t, "go install golang.org/x/tools/gopls@latest", reqs[1].InstallHint)
	assert.Equal(t, "go install golang.org/x/tools/cmd/goimports@latest", reqs[2].InstallHint)
	assert.Empty(t, reqs[3].InstallHint)
	assert.Empty(t, reqs[4].InstallHint)
}

func TestCheckTools_MissingTool(t *testing.T) {
	reqs := []ToolRequirement{
		{Name: "definitely-not-a-real-tool-name-xyz", InstallHint: "install it somehow"},
	}
	st := CheckTools(reqs)
	require.Len(t, st, 1)
	assert.Equal(t, reqs[0].Name, st[0].Name)
	assert.Empty(t, st[0].Path)
	assert.Equal(t, reqs[0].InstallHint, st[0].InstallHint)
}

func TestCheckTools_FindsToolInPATH(t *testing.T) {
	tmp := t.TempDir()

	toolName := "mytool"
	toolFile := toolName
	contents := "#!/bin/sh\nexit 0\n"
	if runtime.GOOS == "windows" {
		toolFile = toolName + ".bat"
		contents = "@echo off\r\nexit /b 0\r\n"
	}

	toolPath := filepath.Join(tmp, toolFile)
	err := os.WriteFile(toolPath, []byte(contents), 0o755)
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		require.NoError(t, os.Chmod(toolPath, 0o755))
	}

	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+oldPath)

	st := CheckTools([]ToolRequirement{{Name: toolName}})
	require.Len(t, st, 1)
	assert.Equal(t, toolName, st[0].Name)
	assert.Equal(t, toolPath, st[0].Path)
}
