package packagemode_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/codeunit"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/prompt"
	"github.com/codalotl/codalotl/internal/subagents/packagemode"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/codalotl/codalotl/internal/tools/toolsets"

	"github.com/stretchr/testify/require"
)

func TestRunIntegration(t *testing.T) {
	if os.Getenv("INTEGRATION_TEST") != "1" {
		t.Skip("integration test disabled; set INTEGRATION_TEST=1 to enable")
	}

	modelID := llmmodel.DefaultModel
	if !modelID.Valid() {
		t.Skipf("model %s is not registered", modelID)
	}

	if strings.TrimSpace(llmmodel.GetAPIKey(modelID)) == "" {
		t.Skipf("no API key configured for %s model", modelID)
	}

	mod, err := gocode.NewModule(gocode.MustCwd())
	require.NoError(t, err)

	pkg, err := mod.LoadPackageByRelativeDir("internal/subagents/packagemode")
	require.NoError(t, err)

	fallback := authdomain.NewAutoApproveAuthorizer(mod.AbsolutePath)
	unit, err := codeunit.NewCodeUnit("packagemode test", pkg.AbsolutePath())
	require.NoError(t, err)
	require.NoError(t, unit.IncludeSubtreeUnlessContains("*.go"))
	unit.PruneEmptyDirs()

	a := authdomain.NewCodeUnitAuthorizer(unit, fallback)

	instructions := "Don't make any changes. Instead, describe the main files and tests in this package."
	answer, err := packagemode.Run(
		context.Background(),
		agent.NewAgentCreator(),
		a,
		pkg.AbsolutePath(),
		toolsets.LimitedPackageAgentTools,
		instructions,
		prompt.GoPackageModePromptKindUpdateUsage,
	)
	require.NoError(t, err)
	require.NotEmpty(t, strings.TrimSpace(answer))

	t.Logf("packagemode response: %s", answer)
}
