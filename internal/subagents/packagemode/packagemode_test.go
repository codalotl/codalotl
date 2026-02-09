package packagemode_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/codeunit"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodetesting"
	"github.com/codalotl/codalotl/internal/lints"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/subagents/packagemode"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/codalotl/codalotl/internal/tools/toolsetinterface"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var dummyToolset toolsetinterface.Toolset = func(toolsetinterface.Options) ([]llmstream.Tool, error) {
	return nil, nil
}

func TestRun_ValidatesInputs(t *testing.T) {
	t.Run("agentCreator required", func(t *testing.T) {
		_, err := packagemode.Run(context.Background(), nil, nil, "/abs", nil, "x", nil, "")
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("authorizer required", func(t *testing.T) {
		_, err := packagemode.Run(context.Background(), agent.NewAgentCreator(), nil, "/abs", nil, "x", nil, "")
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("toolset required", func(t *testing.T) {
		sandbox := t.TempDir()
		a := authdomain.NewAutoApproveAuthorizer(sandbox)
		_, err := packagemode.Run(context.Background(), agent.NewAgentCreator(), a, "/abs", nil, "x", nil, "")
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("instructions required", func(t *testing.T) {
		sandbox := t.TempDir()
		a := authdomain.NewAutoApproveAuthorizer(sandbox)
		_, err := packagemode.Run(context.Background(), agent.NewAgentCreator(), a, "/abs", dummyToolset, "", nil, "")
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("goPkgAbsDir must be absolute", func(t *testing.T) {
		sandbox := t.TempDir()
		a := authdomain.NewAutoApproveAuthorizer(sandbox)
		_, err := packagemode.Run(context.Background(), agent.NewAgentCreator(), a, "relative/path", dummyToolset, "x", nil, "")
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("goPkgAbsDir must be a dir", func(t *testing.T) {
		sandbox := t.TempDir()
		a := authdomain.NewAutoApproveAuthorizer(sandbox)

		p := filepath.Join(t.TempDir(), "file.txt")
		if err := os.WriteFile(p, []byte("hi"), 0o600); err != nil {
			t.Fatalf("write file: %v", err)
		}

		_, err := packagemode.Run(context.Background(), agent.NewAgentCreator(), a, p, dummyToolset, "x", nil, "")
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestRun_ThreadsLintStepsToInitialContext(t *testing.T) {
	gocodetesting.WithCode(t, `func F() {}`, func(pkg *gocode.Package) {
		lintSteps := []lints.Step{{ID: "broken"}}

		_, err := packagemode.Run(
			context.Background(),
			agent.NewAgentCreator(),
			newCodeUnitAuthorizer(t, pkg),
			pkg.AbsolutePath(),
			dummyToolset,
			"x",
			lintSteps,
			"",
		)
		require.Error(t, err)
		assert.Contains(t, err.Error(), `lint step "broken": check command is required`)
	})
}

func TestRun_ThreadsLintStepsToToolsetCreation(t *testing.T) {
	gocodetesting.WithCode(t, `func F() {}`, func(pkg *gocode.Package) {
		lintSteps := make([]lints.Step, 0)
		wantErr := errors.New("stop after toolset options capture")

		var captured toolsetinterface.Options
		toolset := func(opts toolsetinterface.Options) ([]llmstream.Tool, error) {
			captured = opts
			return nil, wantErr
		}

		_, err := packagemode.Run(
			context.Background(),
			agent.NewAgentCreator(),
			newCodeUnitAuthorizer(t, pkg),
			pkg.AbsolutePath(),
			toolset,
			"x",
			lintSteps,
			"",
		)
		require.ErrorIs(t, err, wantErr)
		assert.Equal(t, lintSteps, captured.LintSteps)
	})
}

func newCodeUnitAuthorizer(t *testing.T, pkg *gocode.Package) authdomain.Authorizer {
	t.Helper()

	sandboxAuthorizer := authdomain.NewAutoApproveAuthorizer(pkg.Module.AbsolutePath)
	unit, err := codeunit.NewCodeUnit("test package", pkg.AbsolutePath())
	require.NoError(t, err)
	unit.IncludeEntireSubtree()

	return authdomain.NewCodeUnitAuthorizer(unit, sandboxAuthorizer)
}
