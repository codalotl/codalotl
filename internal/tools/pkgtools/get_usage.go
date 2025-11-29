package pkgtools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodecontext"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/auth"
	"github.com/codalotl/codalotl/internal/tools/coretools"
	"path/filepath"
	"strings"
)

//go:embed get_usage.md
var descriptionGetUsage string

const ToolNameGetUsage = "get_usage"

type toolGetUsage struct {
	sandboxAbsDir string
	authorizer    auth.Authorizer
}

type getUsageParams struct {
	DefiningPackage string `json:"defining_package"`
	Identifier      string `json:"identifier"`
}

func NewGetUsageTool(sandboxAbsDir string, authorizer auth.Authorizer) llmstream.Tool {
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
			"defining_package": map[string]any{
				"type":        "string",
				"description": "The import path of the package defining the identifier.",
			},
			"identifier": map[string]any{
				"type":        "string",
				"description": "The identifier defined in defining_package.",
			},
		},
		Required: []string{"defining_package", "identifier"},
	}
}

func (t *toolGetUsage) Run(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
	_ = ctx

	var params getUsageParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return coretools.NewToolErrorResult(call, fmt.Sprintf("error parsing parameters: %s", err), err)
	}

	if params.DefiningPackage == "" {
		return llmstream.NewErrorToolResult("defining_package is required", call)
	}

	if params.Identifier == "" {
		return llmstream.NewErrorToolResult("identifier is required", call)
	}

	mod, err := gocode.NewModule(t.sandboxAbsDir)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	_, relativeDir, err := resolveImportPath(mod.Name, params.DefiningPackage)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	absPackageDir := mod.AbsolutePath
	if relativeDir != "" {
		absPackageDir = filepath.Join(absPackageDir, filepath.FromSlash(relativeDir))
	}

	if t.authorizer != nil {
		if authErr := t.authorizer.IsAuthorizedForRead(false, "", ToolNameGetUsage, t.sandboxAbsDir, absPackageDir); authErr != nil {
			return coretools.NewToolErrorResult(call, authErr.Error(), authErr)
		}
	}

	_, usageSummary, err := gocodecontext.CrossPackageUsage(absPackageDir, params.Identifier)
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
