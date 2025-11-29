package toolsets

import (
	"fmt"
	"path/filepath"

	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/auth"
	"github.com/codalotl/codalotl/internal/tools/coretools"
	"github.com/codalotl/codalotl/internal/tools/exttools"
	"github.com/codalotl/codalotl/internal/tools/pkgtools"
)

// CoreAgentTools offers tools similar to a Codex-style agent: read_file, ls, apply_patch, shell, and update_plan.
//
// sandboxDir is an absolute path that represents the "jail" that the agent runs in. However, it's `authorizer` that actually
// **implements** the jail. The purpose of accepting sandboxDir here is so that relative paths received by the LLM can be made absolute.
func CoreAgentTools(sandboxDir string, authorizer auth.Authorizer) ([]llmstream.Tool, error) {
	if !filepath.IsAbs(sandboxDir) {
		return nil, fmt.Errorf("sandboxDir must be an absolute path")
	}

	tools := []llmstream.Tool{
		coretools.NewReadFileTool(sandboxDir, authorizer),
		coretools.NewLsTool(sandboxDir, authorizer),
		coretools.NewApplyPatchTool(sandboxDir, authorizer, true, nil),
		coretools.NewShellTool(sandboxDir, authorizer),
		coretools.NewUpdatePlanTool(sandboxDir, authorizer),
	}
	return tools, nil
}

// PackageAgentTools offers tools that jail an agent to one code unit (in Go, typically a package), located at goPkgAbsDir:
//   - core tools: read_file, ls, apply_patch, update_plan
//   - extended tools: diagnostics (i.e., typecheck errors/build errors), fix_lints, run_tests, run_project_tests
//   - package tools: get_public_api, clarify_public_api, get_usage, update_usage
//
// Note that this set of tools requires two authorizers:
//   - authorizer is the package-jail authorizer that prevents the agent from directly accessing files outside the package.
//   - sandboxAuthorizer is the sandboxDir jail. This comes into play when for tools designed to operate outside the package. Notably, `clarify_public_api`, `update_usage`, etc.
//
// sandboxDir is simply the absolute path that relative paths received by the LLM are relative to. It is NOT the package jail dir.
func PackageAgentTools(sandboxDir string, authorizer auth.Authorizer, sandboxAuthorizer auth.Authorizer, goPkgAbsDir string) ([]llmstream.Tool, error) {
	if !filepath.IsAbs(sandboxDir) {
		return nil, fmt.Errorf("sandboxDir must be an absolute path")
	}
	if !filepath.IsAbs(goPkgAbsDir) {
		return nil, fmt.Errorf("goPkgAbsDir must be an absolute path")
	}

	tools := []llmstream.Tool{
		coretools.NewReadFileTool(sandboxDir, authorizer),
		coretools.NewLsTool(sandboxDir, authorizer),
		coretools.NewApplyPatchTool(sandboxDir, authorizer, true, packageApplyPatchPostChecks()),
		coretools.NewUpdatePlanTool(sandboxDir, authorizer),
		exttools.NewDiagnosticsTool(sandboxDir, authorizer),
		exttools.NewFixLintsTool(sandboxDir, authorizer),
		exttools.NewRunTestsTool(sandboxDir, authorizer),
		exttools.NewRunProjectTestsTool(sandboxDir, goPkgAbsDir, authorizer),
		pkgtools.NewGetPublicAPITool(sandboxDir, authorizer),
		pkgtools.NewClarifyPublicAPITool(sandboxDir, sandboxAuthorizer, SimpleReadOnlyTools),
		pkgtools.NewGetUsageTool(sandboxDir, authorizer),
		pkgtools.NewUpdateUsageTool(sandboxDir, goPkgAbsDir, sandboxAuthorizer, LimitedPackageAgentTools),
	}
	return tools, nil
}

// SimpleReadOnlyTools offers ls and read_file. It can excel at a small research task (ex: clarifying documentation inside a package).
//
// sandboxDir is an absolute path that represents the "jail" that the agent runs in. However, it's `authorizer` that actually
// **implements** the jail. The purpose of accepting sandboxDir here is so that relative paths received by the LLM can be made absolute.
func SimpleReadOnlyTools(sandboxDir string, authorizer auth.Authorizer) ([]llmstream.Tool, error) {
	if !filepath.IsAbs(sandboxDir) {
		return nil, fmt.Errorf("sandboxDir must be an absolute path")
	}

	tools := []llmstream.Tool{
		coretools.NewLsTool(sandboxDir, authorizer),
		coretools.NewReadFileTool(sandboxDir, authorizer),
	}
	return tools, nil
}

// LimitedPackageAgentTools offers more limited tools than PackageAgentTools that jail an agent to one code unit (in Go, typically a package), located at goPkgAbsDir:
//   - core tools: read_file, ls, apply_patch (not included: update_plan)
//   - extended tools: diagnostics (i.e., typecheck errors/build errors), fix_lints, run_tests (not included: run_project_tests)
//   - package tools: get_public_api, clarify_public_api (not included: get_usage, update_usage)
//
// These tools cannot spawn write-mode subagents that escape the original goPkgAbsDir (but they can spawn subagents with read access - e.g., clarify_public_api). They
// are intended to be used for subagents running update_usage. In other words, they target small, simple, mechanical code changes on a single package.
//
// See PackageAgentTools for other param descriptions.
func LimitedPackageAgentTools(sandboxDir string, authorizer auth.Authorizer, sandboxAuthorizer auth.Authorizer, goPkgAbsDir string) ([]llmstream.Tool, error) {
	if !filepath.IsAbs(sandboxDir) {
		return nil, fmt.Errorf("sandboxDir must be an absolute path")
	}
	if !filepath.IsAbs(goPkgAbsDir) {
		return nil, fmt.Errorf("goPkgAbsDir must be an absolute path")
	}

	tools := []llmstream.Tool{
		coretools.NewReadFileTool(sandboxDir, authorizer),
		coretools.NewLsTool(sandboxDir, authorizer),
		coretools.NewApplyPatchTool(sandboxDir, authorizer, true, packageApplyPatchPostChecks()),
		exttools.NewDiagnosticsTool(sandboxDir, authorizer),
		exttools.NewFixLintsTool(sandboxDir, authorizer),
		exttools.NewRunTestsTool(sandboxDir, authorizer),
		pkgtools.NewGetPublicAPITool(sandboxDir, authorizer),
		pkgtools.NewClarifyPublicAPITool(sandboxDir, sandboxAuthorizer, SimpleReadOnlyTools),
	}
	_ = goPkgAbsDir // ensure goPkgAbsDir is validated even though no tool currently uses it directly.
	return tools, nil
}

func packageApplyPatchPostChecks() *coretools.ApplyPatchPostChecks {
	return &coretools.ApplyPatchPostChecks{
		RunDiagnostics: exttools.RunDiagnostics,
		FixLints:       exttools.FixLints,
	}
}
