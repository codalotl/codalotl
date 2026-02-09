package toolsets

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/codalotl/codalotl/internal/lints"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/q/cmdrunner"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/codalotl/codalotl/internal/tools/coretools"
	"github.com/codalotl/codalotl/internal/tools/exttools"
	"github.com/codalotl/codalotl/internal/tools/pkgtools"
	"github.com/stretchr/testify/require"
)

func TestCoreAgentTools(t *testing.T) {
	sandbox := t.TempDir()
	auth := authdomain.NewAutoApproveAuthorizer(sandbox)

	tools, err := CoreAgentTools(Options{SandboxDir: sandbox, Authorizer: auth})
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

	tools, err := PackageAgentTools(Options{SandboxDir: sandbox, Authorizer: auth, GoPkgAbsDir: goPkg})
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

	tools, err := SimpleReadOnlyTools(Options{SandboxDir: sandbox, Authorizer: auth})
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

	tools, err := LimitedPackageAgentTools(Options{SandboxDir: sandbox, Authorizer: auth, GoPkgAbsDir: goPkg})
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

func TestPackageAgentTools_ThreadsLintStepsToTools(t *testing.T) {
	t.Setenv("CODALOTL_TOOLSETS_LINTS_HELPER_PROCESS", "1")

	sandbox := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(sandbox, "go.mod"), []byte("module example.com/test\n\ngo 1.22\n"), 0o644))

	pkgDir := filepath.Join(sandbox, "pkg")
	require.NoError(t, os.MkdirAll(pkgDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "pkg.go"), []byte("package pkg\n\nfunc F() {}\n"), 0o644))

	steps := []lints.Step{
		{
			ID:    "custom",
			Check: toolsetsHelperCmd("check", 0),
			Fix:   toolsetsHelperCmd("custom-fix", 0),
		},
	}

	auth := authdomain.NewAutoApproveAuthorizer(sandbox)
	tools, err := PackageAgentTools(Options{SandboxDir: sandbox, Authorizer: auth, GoPkgAbsDir: pkgDir, LintSteps: steps})
	require.NoError(t, err)

	var fixTool llmstream.Tool
	var applyTool llmstream.Tool
	for _, tool := range tools {
		switch tool.Name() {
		case exttools.ToolNameFixLints:
			fixTool = tool
		case coretools.ToolNameApplyPatch:
			applyTool = tool
		}
	}
	require.NotNil(t, fixTool)
	require.NotNil(t, applyTool)

	fixRes := fixTool.Run(context.Background(), llmstream.ToolCall{
		CallID: "fix1",
		Name:   exttools.ToolNameFixLints,
		Type:   "function_call",
		Input:  `{"path":"pkg"}`,
	})
	require.False(t, fixRes.IsError)
	require.Contains(t, fixRes.Result, "custom-fix")

	patch := `*** Begin Patch
*** Update File: pkg/pkg.go
@@
-package pkg
-
-func F() {}
+package pkg
+
+// touch
+func F() {}
*** End Patch`

	applyRes := applyTool.Run(context.Background(), llmstream.ToolCall{
		CallID: "ap1",
		Name:   coretools.ToolNameApplyPatch,
		Type:   "custom_tool_call",
		Input:  patch,
	})
	require.False(t, applyRes.IsError)
	require.Contains(t, applyRes.Result, "<lint-status")
	require.Contains(t, applyRes.Result, "custom-fix")
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

func toolsetsHelperCmd(stdout string, exitCode int) *cmdrunner.Command {
	return &cmdrunner.Command{
		Command: os.Args[0],
		Args: []string{
			"-test.run=^TestToolsetsLintsHelperProcess$",
			"--",
			"stdout=" + stdout,
			"exit=" + strconv.Itoa(exitCode),
		},
		OutcomeFailIfAnyOutput: false,
	}
}

func TestToolsetsLintsHelperProcess(t *testing.T) {
	if os.Getenv("CODALOTL_TOOLSETS_LINTS_HELPER_PROCESS") != "1" {
		return
	}

	args := os.Args
	delimiter := -1
	for i, a := range args {
		if a == "--" {
			delimiter = i
			break
		}
	}
	if delimiter == -1 {
		os.Exit(2)
	}

	var stdout string
	exitCode := 0

	for _, a := range args[delimiter+1:] {
		if strings.HasPrefix(a, "stdout=") {
			stdout = strings.TrimPrefix(a, "stdout=")
			continue
		}
		if strings.HasPrefix(a, "exit=") {
			n, err := strconv.Atoi(strings.TrimPrefix(a, "exit="))
			if err != nil {
				os.Exit(2)
			}
			exitCode = n
			continue
		}
	}

	if stdout != "" {
		_, _ = os.Stdout.WriteString(stdout)
	}
	os.Exit(exitCode)
}
