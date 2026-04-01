package pkgtools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/codalotl/codalotl/internal/codeunit"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/llmstream"
	clarify "github.com/codalotl/codalotl/internal/subagents/clarifydocs"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/codalotl/codalotl/internal/tools/coretools"
	"github.com/codalotl/codalotl/internal/tools/toolsetinterface"
)

//go:embed clarify_public_api.md
var descriptionClarifyPublicAPI string

const ToolNameClarifyPublicAPI = "clarify_public_api"

type toolClarifyPublicAPI struct {
	sandboxAbsDir string
	authorizer    authdomain.Authorizer
	toolset       toolsetinterface.Toolset
}

type clarifyPublicAPIParams struct {
	Path       string `json:"path"`
	Identifier string `json:"identifier"`
	Question   string `json:"question"`
}

// authorizer is the fallback authorizer the clarify subagent should use underneath its target-package jail.
func NewClarifyPublicAPITool(authorizer authdomain.Authorizer, toolset toolsetinterface.Toolset) llmstream.Tool {
	sandboxAbsDir := authorizer.SandboxDir()
	return &toolClarifyPublicAPI{
		sandboxAbsDir: sandboxAbsDir,
		authorizer:    authorizer,
		toolset:       toolset,
	}
}

func (t *toolClarifyPublicAPI) Name() string {
	return ToolNameClarifyPublicAPI
}

func (t *toolClarifyPublicAPI) Info() llmstream.ToolInfo {
	return llmstream.ToolInfo{
		Name:        ToolNameClarifyPublicAPI,
		Description: strings.TrimSpace(descriptionClarifyPublicAPI),
		Parameters: map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "A Go package directory (relative to the sandbox) or a Go import path.",
			},
			"identifier": map[string]any{
				"type":        "string",
				"description": "The identifier needing clarification.",
			},
			"question": map[string]any{
				"type":        "string",
				"description": "The specific clarification question.",
			},
		},
		Required: []string{"path", "identifier", "question"},
	}
}

func (t *toolClarifyPublicAPI) Run(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
	var params clarifyPublicAPIParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return coretools.NewToolErrorResult(call, fmt.Sprintf("error parsing parameters: %s", err), err)
	}

	if params.Path == "" {
		return llmstream.NewErrorToolResult("path is required", call)
	}
	if params.Identifier == "" {
		return llmstream.NewErrorToolResult("identifier is required", call)
	}
	if params.Question == "" {
		return llmstream.NewErrorToolResult("question is required", call)
	}

	mod, err := gocode.NewModule(t.sandboxAbsDir)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	moduleAbsDir, packageAbsDir, _, _, err := resolvePackagePath(mod, params.Path)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	effectiveSandboxAbsDir := t.sandboxAbsDir
	baseAuthorizer := t.authorizer
	if baseAuthorizer != nil {
		baseAuthorizer = baseAuthorizer.WithoutCodeUnit()
	}
	if !isWithinDir(t.sandboxAbsDir, packageAbsDir) {
		// If the resolved package is outside of the primary sandbox, treat its module (or stdlib root)
		// as the sandbox root for relative path resolution in the clarify subagent.
		if moduleAbsDir != "" {
			effectiveSandboxAbsDir = moduleAbsDir
		} else {
			stdRootAbsDir, _ := stdlibRootAndRel(packageAbsDir)
			if stdRootAbsDir != "" {
				effectiveSandboxAbsDir = stdRootAbsDir
			}
		}

		// Some authorizers enforce sandbox membership. If we can, clone it with an updated sandbox
		// to allow read access to the resolved dependency/stdlib package while still confining reads.
		if baseAuthorizer != nil && effectiveSandboxAbsDir != "" {
			if updated, updErr := authdomain.WithUpdatedSandbox(baseAuthorizer, effectiveSandboxAbsDir); updErr == nil {
				baseAuthorizer = updated
			}
		}
	}

	effectiveAuthorizer, err := newClarifyTargetAuthorizer(baseAuthorizer, packageAbsDir)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	if t.authorizer != nil && isWithinDir(t.sandboxAbsDir, packageAbsDir) {
		// Only prompt/deny for sandbox reads; resolved dependency/stdlib packages are always readable.
		if authErr := t.authorizer.IsAuthorizedForRead(false, "", ToolNameClarifyPublicAPI, packageAbsDir); authErr != nil {
			return coretools.NewToolErrorResult(call, authErr.Error(), authErr)
		}
	}

	agentCreator, err := subAgentCreatorFromContextSafe(ctx)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	answer, err := clarify.ClarifyAPI(ctx, agentCreator, effectiveSandboxAbsDir, effectiveAuthorizer, t.toolset, packageAbsDir, params.Identifier, params.Question)
	if err != nil {
		return coretools.NewToolErrorResult(call, err.Error(), err)
	}

	return llmstream.ToolResult{
		CallID: call.CallID,
		Name:   call.Name,
		Type:   call.Type,
		Result: answer,
	}
}

func newClarifyTargetAuthorizer(baseAuthorizer authdomain.Authorizer, packageAbsDir string) (authdomain.Authorizer, error) {
	if baseAuthorizer == nil {
		return nil, nil
	}
	if packageAbsDir == "" {
		return nil, fmt.Errorf("package path is required")
	}

	unit, err := codeunit.NewCodeUnit(fmt.Sprintf("package %s", packageAbsDir), packageAbsDir)
	if err != nil {
		return nil, fmt.Errorf("build code unit: %w", err)
	}
	if err := unit.IncludeSubtreeUnlessContains("*.go"); err != nil {
		return nil, fmt.Errorf("expand code unit subtree: %w", err)
	}
	if err := includeReachableTestdataDirs(unit); err != nil {
		return nil, fmt.Errorf("include reachable testdata dirs: %w", err)
	}
	return authdomain.NewCodeUnitAuthorizer(unit, baseAuthorizer), nil
}

func includeReachableTestdataDirs(unit *codeunit.CodeUnit) error {
	if unit == nil {
		return nil
	}

	for _, absPath := range unit.IncludedFiles() {
		info, err := os.Stat(absPath)
		if err != nil || !info.IsDir() {
			continue
		}

		testdataPath := filepath.Join(absPath, "testdata")
		tdInfo, err := os.Stat(testdataPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("stat %q: %w", testdataPath, err)
		}
		if !tdInfo.IsDir() || unit.Includes(testdataPath) {
			continue
		}
		if err := unit.IncludeDir(testdataPath, true); err != nil {
			return fmt.Errorf("include %q: %w", testdataPath, err)
		}
	}

	return nil
}
