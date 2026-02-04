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

//go:embed get_usage.md
var descriptionGetUsage string

const ToolNameGetUsage = "get_usage"

type toolGetUsage struct {
	sandboxAbsDir string
	authorizer    authdomain.Authorizer
}

type getUsageParams struct {
	DefiningPackagePath string `json:"defining_package_path"`
	Identifier          string `json:"identifier"`
}

func NewGetUsageTool(authorizer authdomain.Authorizer) llmstream.Tool {
	sandboxAbsDir := authorizer.SandboxDir()
	return &toolGetUsage{
		sandboxAbsDir: sandboxAbsDir,
		authorizer:    authorizer,
	}
}

func (t *toolGetUsage) Name() string {
	return ToolNameGetUsage
}

func (t *toolGetUsage) Info() llmstream.ToolInfo {
	return llmstream.ToolInfo{
		Name:        ToolNameGetUsage,
		Description: strings.TrimSpace(descriptionGetUsage),
		Parameters: map[string]any{
			"defining_package_path": map[string]any{
				"type":        "string",
				"description": "A Go package directory (relative to the sandbox) or a Go import path. Must resolve to the package defining the identifier.",
			},
			"identifier": map[string]any{
				"type":        "string",
				"description": "The identifier defined in defining_package_path.",
			},
		},
		Required: []string{"defining_package_path", "identifier"},
	}
}

func (t *toolGetUsage) Run(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
	_ = ctx

	var params getUsageParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return coretools.NewToolErrorResult(call, fmt.Sprintf("error parsing parameters: %s", err), err)
	}

	if params.DefiningPackagePath == "" {
		return llmstream.NewErrorToolResult("defining_package_path is required", call)
	}

	if params.Identifier == "" {
		return llmstream.NewErrorToolResult("identifier is required", call)
	}

	mod, err := gocode.NewModule(t.sandboxAbsDir)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	_, packageAbsDir, _, _, err := resolvePackagePath(mod, params.DefiningPackagePath)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	if t.authorizer != nil && isWithinDir(t.sandboxAbsDir, packageAbsDir) {
		// Only prompt/deny for sandbox reads; resolved dependency/stdlib packages are always readable.
		if authErr := t.authorizer.IsAuthorizedForRead(false, "", ToolNameGetUsage, packageAbsDir); authErr != nil {
			return coretools.NewToolErrorResult(call, authErr.Error(), authErr)
		}
	}

	_, usageSummary, err := gocodecontext.CrossPackageUsage(packageAbsDir, params.Identifier)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	return llmstream.ToolResult{
		CallID: call.CallID,
		Name:   call.Name,
		Type:   call.Type,
		Result: usageSummary,
	}
}
