package exttools

import (
	"testing"

	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"

	"github.com/stretchr/testify/assert"
)

var (
	_ llmstream.Tool = (*toolDiagnostics)(nil)
	_ llmstream.Tool = (*toolFixLints)(nil)
	_ llmstream.Tool = (*toolRunProjectTests)(nil)
	_ llmstream.Tool = (*toolRunTests)(nil)
)

func TestTools_Presenter_IsNil(t *testing.T) {
	auth := authdomain.NewAutoApproveAuthorizer(t.TempDir())

	tools := []llmstream.Tool{
		NewDiagnosticsTool(auth),
		NewFixLintsTool(auth, nil),
		NewRunProjectTestsTool("", auth),
		NewRunTestsTool(auth, nil),
	}

	for _, tool := range tools {
		assert.Nil(t, tool.Presenter())
	}
}
