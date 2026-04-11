package coretools

import (
	"testing"

	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/stretchr/testify/assert"
)

func TestToolsPresenterIsNil(t *testing.T) {
	sandbox := t.TempDir()
	auth := authdomain.NewAutoApproveAuthorizer(sandbox)

	tools := []llmstream.Tool{
		NewApplyPatchTool(auth, false, nil),
		NewApplyPatchTool(auth, true, nil),
		NewDeleteTool(auth),
		NewEditTool(auth),
		NewLsTool(auth),
		NewReadFileTool(auth),
		NewShellTool(auth),
		NewSkillShellTool(auth),
		NewUpdatePlanTool(auth),
		NewWriteTool(auth),
	}

	for _, tool := range tools {
		assert.Nil(t, tool.Presenter())
	}
}
