package toolsets

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/codalotl/codalotl/internal/tools/coretools"
	"github.com/codalotl/codalotl/internal/tools/exttools"
	"github.com/codalotl/codalotl/internal/tools/pkgtools"
)

func TestCoreAgentTools(t *testing.T) {
	sandbox := t.TempDir()
	auth := authdomain.NewAutoApproveAuthorizer(sandbox)

	tools, err := CoreAgentTools(sandbox, auth)
	if err != nil {
		t.Fatalf("CoreAgentTools returned error: %v", err)
	}

	assertToolNames(t, tools, []string{
		coretools.ToolNameReadFile,
		coretools.ToolNameLS,
		coretools.ToolNameApplyPatch,
		coretools.ToolNameShell,
		coretools.ToolNameUpdatePlan,
	})
}

func TestPackageAgentTools(t *testing.T) {
	sandbox := t.TempDir()
	auth := authdomain.NewAutoApproveAuthorizer(sandbox)
	goPkg := filepath.Join(sandbox, "pkg")
	if err := os.MkdirAll(goPkg, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", goPkg, err)
	}

	tools, err := PackageAgentTools(sandbox, auth, goPkg)
	if err != nil {
		t.Fatalf("PackageAgentTools returned error: %v", err)
	}

	assertToolNames(t, tools, []string{
		coretools.ToolNameReadFile,
		coretools.ToolNameLS,
		coretools.ToolNameApplyPatch,
		coretools.ToolNameUpdatePlan,
		exttools.ToolNameDiagnostics,
		exttools.ToolNameFixLints,
		exttools.ToolNameRunTests,
		exttools.ToolNameRunProjectTests,
		pkgtools.ToolNameModuleInfo,
		pkgtools.ToolNameGetPublicAPI,
		pkgtools.ToolNameClarifyPublicAPI,
		pkgtools.ToolNameGetUsage,
		pkgtools.ToolNameUpdateUsage,
		pkgtools.ToolNameChangeAPI,
	})
}

func TestSimpleReadOnlyTools(t *testing.T) {
	sandbox := t.TempDir()
	auth := authdomain.NewAutoApproveAuthorizer(sandbox)

	tools, err := SimpleReadOnlyTools(sandbox, auth)
	if err != nil {
		t.Fatalf("SimpleReadOnlyTools returned error: %v", err)
	}

	assertToolNames(t, tools, []string{
		coretools.ToolNameLS,
		coretools.ToolNameReadFile,
	})
}

func TestLimitedPackageAgentTools(t *testing.T) {
	sandbox := t.TempDir()
	auth := authdomain.NewAutoApproveAuthorizer(sandbox)
	goPkg := filepath.Join(sandbox, "pkg")
	if err := os.MkdirAll(goPkg, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", goPkg, err)
	}

	tools, err := LimitedPackageAgentTools(sandbox, auth, goPkg)
	if err != nil {
		t.Fatalf("LimitedPackageAgentTools returned error: %v", err)
	}

	assertToolNames(t, tools, []string{
		coretools.ToolNameReadFile,
		coretools.ToolNameLS,
		coretools.ToolNameApplyPatch,
		exttools.ToolNameDiagnostics,
		exttools.ToolNameFixLints,
		exttools.ToolNameRunTests,
		pkgtools.ToolNameGetPublicAPI,
		pkgtools.ToolNameClarifyPublicAPI,
	})
}

func assertToolNames(t *testing.T, tools []llmstream.Tool, want []string) {
	t.Helper()

	got := make([]string, len(tools))
	for i, tool := range tools {
		got[i] = tool.Name()
	}

	if len(got) != len(want) {
		t.Fatalf("tool count mismatch: got %d, want %d (names=%v)", len(got), len(want), got)
	}

	for i, name := range want {
		if got[i] != name {
			t.Fatalf("tool[%d] mismatch: got %q, want %q (all=%v)", i, got[i], name, got)
		}
	}
}
