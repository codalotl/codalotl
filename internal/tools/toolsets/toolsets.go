package toolsets

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/codalotl/codalotl/internal/lints"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/coretools"
	"github.com/codalotl/codalotl/internal/tools/exttools"
	"github.com/codalotl/codalotl/internal/tools/pkgtools"
	"github.com/codalotl/codalotl/internal/tools/toolsetinterface"
)

// Options configures the toolset returned by the functions in this package.
//
// This is an alias to toolsetinterface.Options so that:
//   - external callers can depend on toolsets.Options, and
//   - toolsets.* functions can be passed directly as toolsetinterface.Toolset values.
type Options = toolsetinterface.Options

// CoreAgentTools offers tools similar to a Codex-style agent: read_file, ls, apply_patch, shell, and update_plan.
//
// sandboxDir is an absolute path that represents the "jail" that the agent runs in. However, it's `authorizer` that actually
// **implements** the jail. The purpose of accepting sandboxDir here is so that relative paths received by the LLM can be made absolute.
func CoreAgentTools(opts Options) ([]llmstream.Tool, error) {
	var err error
	opts, err = normalizeSandboxDir(opts)
	if err != nil {
		return nil, err
	}

	authorizer := opts.Authorizer

	tools := []llmstream.Tool{
		coretools.NewReadFileTool(authorizer),
		coretools.NewLsTool(authorizer),
		coretools.NewApplyPatchTool(authorizer, true, nil),
		coretools.NewShellTool(authorizer),
		coretools.NewUpdatePlanTool(authorizer),
	}
	return tools, nil
}

func normalizeSandboxDir(opts Options) (Options, error) {
	// Some internal callers only have an authorizer in hand; it's a reasonable default.
	if opts.SandboxDir == "" && opts.Authorizer != nil {
		opts.SandboxDir = opts.Authorizer.SandboxDir()
	}

	if !filepath.IsAbs(opts.SandboxDir) {
		return Options{}, fmt.Errorf("sandboxDir must be an absolute path")
	}
	return opts, nil
}

// PackageAgentTools offers tools that jail an agent to one code unit (in Go, typically a package), located at goPkgAbsDir:
//   - core tools: read_file, ls, apply_patch, update_plan
//   - extended tools: diagnostics (i.e., typecheck errors/build errors), fix_lints, run_tests, run_project_tests
//   - package tools: module_info, get_public_api, clarify_public_api, get_usage, update_usage, change_api
//
// Note that this set of tools requires a package-jail authorizer that prevents the agent from directly accessing files outside
// the package. Tools that need broader sandbox access derive it via authorizer.WithoutCodeUnit().
//
// sandboxDir is simply the absolute path that relative paths received by the LLM are relative to. It is NOT the package
// jail dir.
func PackageAgentTools(opts Options) ([]llmstream.Tool, error) {
	var err error
	opts, err = normalizeSandboxDir(opts)
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(opts.GoPkgAbsDir) {
		return nil, fmt.Errorf("goPkgAbsDir must be an absolute path")
	}

	authorizer := opts.Authorizer
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
		exttools.NewRunProjectTestsTool(opts.GoPkgAbsDir, authorizer),
		pkgtools.NewModuleInfoTool(authorizer),
		pkgtools.NewGetPublicAPITool(authorizer),
		pkgtools.NewClarifyPublicAPITool(sandboxAuthorizer, SimpleReadOnlyTools),
		pkgtools.NewGetUsageTool(authorizer),
		pkgtools.NewUpdateUsageTool(opts.GoPkgAbsDir, sandboxAuthorizer, LimitedPackageAgentTools, opts.LintSteps),
		pkgtools.NewChangeAPITool(opts.GoPkgAbsDir, sandboxAuthorizer, PackageAgentTools, opts.LintSteps),
	}
	return tools, nil
}

// SimpleReadOnlyTools offers ls and read_file. It can excel at a small research task (ex: clarifying documentation inside
// a package).
//
// sandboxDir is an absolute path that represents the "jail" that the agent runs in. However, it's authorizer that actually
// implements the jail. The purpose of accepting sandboxDir here is so that relative paths received by the LLM can be made
// absolute.
func SimpleReadOnlyTools(opts Options) ([]llmstream.Tool, error) {
	var err error
	opts, err = normalizeSandboxDir(opts)
	if err != nil {
		return nil, err
	}

	authorizer := opts.Authorizer

	tools := []llmstream.Tool{
		coretools.NewLsTool(authorizer),
		coretools.NewReadFileTool(authorizer),
	}
	return tools, nil
}

// LimitedPackageAgentTools offers more limited tools than PackageAgentTools that jail an agent to one code unit (in Go,
// typically a package), located at goPkgAbsDir:
//   - core tools: read_file, ls, apply_patch (not included: update_plan)
//   - extended tools: diagnostics (i.e., typecheck errors/build errors), fix_lints, run_tests (not included: run_project_tests)
//   - package tools: get_public_api, clarify_public_api (not included: get_usage, update_usage)
//
// These tools cannot spawn write-mode subagents that escape the original goPkgAbsDir (but they can spawn subagents with
// read access - e.g., clarify_public_api). They are intended to be used for subagents running update_usage. In other words,
// they target small, simple, mechanical code changes on a single package.
//
// See PackageAgentTools for other param descriptions.
func LimitedPackageAgentTools(opts Options) ([]llmstream.Tool, error) {
	var err error
	opts, err = normalizeSandboxDir(opts)
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(opts.GoPkgAbsDir) {
		return nil, fmt.Errorf("goPkgAbsDir must be an absolute path")
	}

	authorizer := opts.Authorizer
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
			// This is the "auto-fix after apply_patch" lint run, not an explicit `fix_lints` tool invocation.
			return lints.Run(ctx, sandboxDir, targetDir, lintSteps, lints.SituationPatch)
		},
	}
}
