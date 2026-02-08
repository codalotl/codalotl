package toolsets

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/codalotl/codalotl/internal/lints"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/codalotl/codalotl/internal/tools/coretools"
	"github.com/codalotl/codalotl/internal/tools/exttools"
	"github.com/codalotl/codalotl/internal/tools/pkgtools"
)

type ToolsetOptions struct {
	// LintSteps configures the lint pipeline used by tools like `fix_lints` and
	// apply_patch post-checks.
	//
	// Nil means "use defaults". An empty slice means "no linters".
	LintSteps []lints.Step
}

// CoreAgentTools offers tools similar to a Codex-style agent: read_file, ls, apply_patch, shell, and update_plan.
//
// sandboxDir is an absolute path that represents the "jail" that the agent runs in. However, it's `authorizer` that actually
// **implements** the jail. The purpose of accepting sandboxDir here is so that relative paths received by the LLM can be made absolute.
func CoreAgentTools(sandboxDir string, authorizer authdomain.Authorizer) ([]llmstream.Tool, error) {
	return CoreAgentToolsWithOptions(sandboxDir, authorizer, ToolsetOptions{})
}

func CoreAgentToolsWithOptions(sandboxDir string, authorizer authdomain.Authorizer, _ ToolsetOptions) ([]llmstream.Tool, error) {
	if !filepath.IsAbs(sandboxDir) {
		return nil, fmt.Errorf("sandboxDir must be an absolute path")
	}

	tools := []llmstream.Tool{
		coretools.NewReadFileTool(authorizer),
		coretools.NewLsTool(authorizer),
		coretools.NewApplyPatchTool(authorizer, true, nil),
		coretools.NewShellTool(authorizer),
		coretools.NewUpdatePlanTool(authorizer),
	}
	return tools, nil
}

// PackageAgentTools offers tools that jail an agent to one code unit (in Go, typically a package), located at goPkgAbsDir:
//   - core tools: read_file, ls, apply_patch, update_plan
//   - extended tools: diagnostics (i.e., typecheck errors/build errors), fix_lints, run_tests, run_project_tests
//   - package tools: module_info, get_public_api, clarify_public_api, get_usage, update_usage, change_api
//
// Note that this set of tools requires a package-jail authorizer that prevents the agent from directly
// accessing files outside the package. Tools that need broader sandbox access derive it via
// authorizer.WithoutCodeUnit().
//
// sandboxDir is simply the absolute path that relative paths received by the LLM are relative to. It is NOT the package jail dir.
func PackageAgentTools(sandboxDir string, authorizer authdomain.Authorizer, goPkgAbsDir string) ([]llmstream.Tool, error) {
	return PackageAgentToolsWithOptions(sandboxDir, authorizer, goPkgAbsDir, ToolsetOptions{})
}

func PackageAgentToolsWithOptions(sandboxDir string, authorizer authdomain.Authorizer, goPkgAbsDir string, opts ToolsetOptions) ([]llmstream.Tool, error) {
	if !filepath.IsAbs(sandboxDir) {
		return nil, fmt.Errorf("sandboxDir must be an absolute path")
	}
	if !filepath.IsAbs(goPkgAbsDir) {
		return nil, fmt.Errorf("goPkgAbsDir must be an absolute path")
	}

	sandboxAuthorizer := authorizer
	if sandboxAuthorizer != nil {
		sandboxAuthorizer = sandboxAuthorizer.WithoutCodeUnit()
	}

	lintSteps := lintStepsOrDefault(opts.LintSteps)

	tools := []llmstream.Tool{
		coretools.NewReadFileTool(authorizer),
		coretools.NewLsTool(authorizer),
		coretools.NewApplyPatchTool(authorizer, true, packageApplyPatchPostChecks(lintSteps)),
		coretools.NewUpdatePlanTool(authorizer),
		exttools.NewDiagnosticsTool(authorizer),
		exttools.NewFixLintsTool(authorizer, lintSteps),
		exttools.NewRunTestsTool(authorizer),
		exttools.NewRunProjectTestsTool(goPkgAbsDir, authorizer),
		pkgtools.NewModuleInfoTool(authorizer),
		pkgtools.NewGetPublicAPITool(authorizer),
		pkgtools.NewClarifyPublicAPITool(sandboxAuthorizer, SimpleReadOnlyTools),
		pkgtools.NewGetUsageTool(authorizer),
		pkgtools.NewUpdateUsageTool(goPkgAbsDir, sandboxAuthorizer, LimitedPackageAgentTools),
		pkgtools.NewChangeAPITool(goPkgAbsDir, sandboxAuthorizer, PackageAgentTools),
	}
	return tools, nil
}

// SimpleReadOnlyTools offers ls and read_file. It can excel at a small research task (ex: clarifying documentation inside a package).
//
// sandboxDir is an absolute path that represents the "jail" that the agent runs in. However, it's `authorizer` that actually
// **implements** the jail. The purpose of accepting sandboxDir here is so that relative paths received by the LLM can be made absolute.
func SimpleReadOnlyTools(sandboxDir string, authorizer authdomain.Authorizer) ([]llmstream.Tool, error) {
	if !filepath.IsAbs(sandboxDir) {
		return nil, fmt.Errorf("sandboxDir must be an absolute path")
	}

	tools := []llmstream.Tool{
		coretools.NewLsTool(authorizer),
		coretools.NewReadFileTool(authorizer),
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
func LimitedPackageAgentTools(sandboxDir string, authorizer authdomain.Authorizer, goPkgAbsDir string) ([]llmstream.Tool, error) {
	return LimitedPackageAgentToolsWithOptions(sandboxDir, authorizer, goPkgAbsDir, ToolsetOptions{})
}

func LimitedPackageAgentToolsWithOptions(sandboxDir string, authorizer authdomain.Authorizer, goPkgAbsDir string, opts ToolsetOptions) ([]llmstream.Tool, error) {
	if !filepath.IsAbs(sandboxDir) {
		return nil, fmt.Errorf("sandboxDir must be an absolute path")
	}
	if !filepath.IsAbs(goPkgAbsDir) {
		return nil, fmt.Errorf("goPkgAbsDir must be an absolute path")
	}

	sandboxAuthorizer := authorizer
	if sandboxAuthorizer != nil {
		sandboxAuthorizer = sandboxAuthorizer.WithoutCodeUnit()
	}

	lintSteps := lintStepsOrDefault(opts.LintSteps)

	tools := []llmstream.Tool{
		coretools.NewReadFileTool(authorizer),
		coretools.NewLsTool(authorizer),
		coretools.NewApplyPatchTool(authorizer, true, packageApplyPatchPostChecks(lintSteps)),
		exttools.NewDiagnosticsTool(authorizer),
		exttools.NewFixLintsTool(authorizer, lintSteps),
		exttools.NewRunTestsTool(authorizer),
		pkgtools.NewGetPublicAPITool(authorizer),
		pkgtools.NewClarifyPublicAPITool(sandboxAuthorizer, SimpleReadOnlyTools),
	}
	_ = goPkgAbsDir // ensure goPkgAbsDir is validated even though no tool currently uses it directly.
	return tools, nil
}

func lintStepsOrDefault(steps []lints.Step) []lints.Step {
	if steps == nil {
		return lints.DefaultSteps()
	}
	return steps
}

func packageApplyPatchPostChecks(lintSteps []lints.Step) *coretools.ApplyPatchPostChecks {
	return &coretools.ApplyPatchPostChecks{
		RunDiagnostics: exttools.RunDiagnostics,
		FixLints: func(ctx context.Context, sandboxDir string, targetDir string) (string, error) {
			if ctx == nil {
				ctx = context.Background()
			}
			return lints.Run(ctx, sandboxDir, targetDir, lintSteps, lints.ActionFix)
		},
	}
}
