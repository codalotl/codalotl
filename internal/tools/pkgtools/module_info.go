package pkgtools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodecontext"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/codalotl/codalotl/internal/tools/coretools"
)

//go:embed module_info.md
var descriptionModuleInfo string

// ToolNameModuleInfo is the registered name of the module_info tool.
const ToolNameModuleInfo = "module_info"

// The toolModuleInfo type implements the module_info tool.
type toolModuleInfo struct {
	sandboxAbsDir string                // The sandbox root is used to locate the Go module.
	authorizer    authdomain.Authorizer // The authorizer controls access to module and package information.
}

// moduleInfoParams contains the JSON parameters for a module_info call. All fields are optional.
type moduleInfoParams struct {
	PackageSearch             string `json:"package_search"`              // PackageSearch filters the returned package list when non-empty.
	IncludeDependencyPackages bool   `json:"include_dependency_packages"` // IncludeDependencyPackages includes packages from direct dependency modules.
}

var moduleInfoPresenterInstance llmstream.Presenter = moduleInfoPresenter{}

// The moduleInfoPresenter type formats module_info tool summaries and call options.
type moduleInfoPresenter struct{}

// NewModuleInfoTool returns a module_info tool that locates the module from the authorizer sandbox and authorizes module information reads with authorizer.
//
// The authorizer must be non-nil.
func NewModuleInfoTool(authorizer authdomain.Authorizer) llmstream.Tool {
	sandboxAbsDir := authorizer.SandboxDir()
	return &toolModuleInfo{
		sandboxAbsDir: sandboxAbsDir,
		authorizer:    authorizer,
	}
}

// Name returns the registered tool name, "module_info".
func (t *toolModuleInfo) Name() string {
	return ToolNameModuleInfo
}

// Presenter returns the presenter that formats module_info calls and results.
func (t *toolModuleInfo) Presenter() llmstream.Presenter {
	return moduleInfoPresenterInstance
}

// Present returns the module_info presentation for call. The presentation replaces prior progress, uses the "Read Module Info" summary, and includes supplied call
// options in the body when present.
func (t moduleInfoPresenter) Present(call llmstream.ToolCall, result *llmstream.ToolResult) llmstream.Presentation {
	presentation := pkgToolReplaceSummaryPresentation(pkgToolActionSummary("Read Module Info"))
	if body, ok := moduleInfoPresenterBody(call); ok {
		presentation.Body = body
	}
	return presentation
}

func moduleInfoPresenterBody(call llmstream.ToolCall) (llmstream.Paragraph, bool) {
	options, ok := moduleInfoPresenterOptionsText(call)
	if !ok {
		return llmstream.Paragraph{}, false
	}
	return pkgToolAccentParagraph(options)
}

// moduleInfoPresenterOptionsText returns the display text for module_info call options. The boolean is false when the call input is invalid or contains no options
// worth displaying.
func moduleInfoPresenterOptionsText(call llmstream.ToolCall) (string, bool) {
	input := call.Input
	if strings.TrimSpace(input) == "" {
		input = "{}"
	}

	var params moduleInfoParams
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", false
	}

	parts := make([]string, 0, 2)
	search := strings.TrimSpace(params.PackageSearch)
	if search != "" {
		parts = append(parts, "Search: "+search)
	}
	if params.IncludeDependencyPackages {
		parts = append(parts, "Deps: true")
	}
	if len(parts) == 0 {
		return "", false
	}
	return strings.Join(parts, "; "), true
}

// Info returns the LLM-facing metadata for the module_info tool, including its optional package search and dependency-package parameters.
func (t *toolModuleInfo) Info() llmstream.ToolInfo {
	return llmstream.ToolInfo{
		Name:        ToolNameModuleInfo,
		Description: strings.TrimSpace(descriptionModuleInfo),
		Parameters: map[string]any{
			"package_search": map[string]any{
				"type":        "string",
				"description": "Optional Go RE2 regexp used to filter the package list.",
			},
			"include_dependency_packages": map[string]any{
				"type":        "boolean",
				"description": "If true, also include packages from direct (non-// indirect) dependency modules.",
			},
		},
	}
}

// Run executes the module_info tool call and returns Go module metadata and a package list. The call input may be empty or JSON containing optional package_search
// and include_dependency_packages fields. package_search is used as a Go regexp filter, and include_dependency_packages adds packages from direct dependency modules.
// Run returns an error tool result for invalid input, module discovery or authorization failures, or module and package listing errors.
func (t *toolModuleInfo) Run(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
	_ = ctx

	input := call.Input
	if strings.TrimSpace(input) == "" {
		input = "{}"
	}

	var params moduleInfoParams
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return coretools.NewToolErrorResult(call, fmt.Sprintf("error parsing parameters: %s", err), err)
	}

	mod, err := gocode.NewModule(t.sandboxAbsDir)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	// module_info is intentionally module-scoped, not package-scoped.
	// Some environments may prompt for permission if dependency packages are included.
	if t.authorizer != nil {
		if authErr := t.authorizer.IsAuthorizedForRead(params.IncludeDependencyPackages, "", ToolNameModuleInfo, mod.AbsolutePath); authErr != nil {
			return coretools.NewToolErrorResult(call, authErr.Error(), authErr)
		}
	}

	modInfo, err := gocodecontext.ModuleInfo(mod.AbsolutePath)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	pkgs, pkgsContext, err := gocodecontext.PackageList(mod.AbsolutePath, params.PackageSearch, params.IncludeDependencyPackages)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	var b strings.Builder
	b.WriteString("Go module information (from go.mod):\n")
	b.WriteString(strings.TrimSpace(modInfo))
	b.WriteString("\n\n")

	b.WriteString(fmt.Sprintf("Packages (%d)", len(pkgs)))
	if strings.TrimSpace(params.PackageSearch) != "" {
		b.WriteString(fmt.Sprintf(" matching %q", params.PackageSearch))
	}
	if params.IncludeDependencyPackages {
		b.WriteString(" (including direct dependency modules)")
	}
	b.WriteString(":\n")

	if len(pkgs) == 0 {
		b.WriteString("(no matching packages)\n")
	} else {
		for _, p := range pkgs {
			b.WriteString("- ")
			b.WriteString(p)
			b.WriteString("\n")
		}
	}

	// Include the opaque context form too; it may contain additional helpful detail.
	if strings.TrimSpace(pkgsContext) != "" {
		b.WriteString("\nPackage list context:\n")
		b.WriteString(strings.TrimSpace(pkgsContext))
		b.WriteString("\n")
	}

	return llmstream.ToolResult{
		CallID: call.CallID,
		Name:   call.Name,
		Type:   call.Type,
		Result: b.String(),
	}
}
