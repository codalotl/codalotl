package pkgtools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodecontext"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/codalotl/codalotl/internal/tools/coretools"
)

//go:embed module_info.md
var descriptionModuleInfo string

const ToolNameModuleInfo = "module_info"

type toolModuleInfo struct {
	sandboxAbsDir string
	authorizer    authdomain.Authorizer
}

type moduleInfoParams struct {
	PackageSearch             string `json:"package_search"`
	IncludeDependencyPackages bool   `json:"include_dependency_packages"`
}

func NewModuleInfoTool(sandboxAbsDir string, authorizer authdomain.Authorizer) llmstream.Tool {
	return &toolModuleInfo{
		sandboxAbsDir: filepath.Clean(sandboxAbsDir),
		authorizer:    authorizer,
	}
}

func (t *toolModuleInfo) Name() string {
	return ToolNameModuleInfo
}

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
