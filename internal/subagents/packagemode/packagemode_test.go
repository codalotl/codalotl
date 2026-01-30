package packagemode_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/subagents/packagemode"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/codalotl/codalotl/internal/tools/toolsetinterface"
)

var dummyToolset toolsetinterface.PackageToolset = func(string, authdomain.Authorizer, string) ([]llmstream.Tool, error) {
	return nil, nil
}

func TestRun_ValidatesInputs(t *testing.T) {
	t.Run("agentCreator required", func(t *testing.T) {
		_, err := packagemode.Run(context.Background(), nil, nil, "/abs", nil, "x", "")
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("authorizer required", func(t *testing.T) {
		_, err := packagemode.Run(context.Background(), agent.NewAgentCreator(), nil, "/abs", nil, "x", "")
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("toolset required", func(t *testing.T) {
		sandbox := t.TempDir()
		a := authdomain.NewAutoApproveAuthorizer(sandbox)
		_, err := packagemode.Run(context.Background(), agent.NewAgentCreator(), a, "/abs", nil, "x", "")
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("instructions required", func(t *testing.T) {
		sandbox := t.TempDir()
		a := authdomain.NewAutoApproveAuthorizer(sandbox)
		_, err := packagemode.Run(context.Background(), agent.NewAgentCreator(), a, "/abs", dummyToolset, "", "")
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("goPkgAbsDir must be absolute", func(t *testing.T) {
		sandbox := t.TempDir()
		a := authdomain.NewAutoApproveAuthorizer(sandbox)
		_, err := packagemode.Run(context.Background(), agent.NewAgentCreator(), a, "relative/path", dummyToolset, "x", "")
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

		_, err := packagemode.Run(context.Background(), agent.NewAgentCreator(), a, p, dummyToolset, "x", "")
		if err == nil {
			t.Fatal("expected error")
		}
	})
}
