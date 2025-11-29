package updateusage_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/subagents/updateusage"
	"github.com/codalotl/codalotl/internal/tools/toolsets"

	"github.com/stretchr/testify/require"
)

func TestUpdateUsageIntegration(t *testing.T) {
	if os.Getenv("INTEGRATION_TEST") != "1" {
		t.Skip("integration test disabled; set INTEGRATION_TEST=1 to enable")
	}

	modelID := llmmodel.ModelID("gpt-5")
	if !modelID.Valid() {
		t.Skip("model gpt-5 is not registered")
	}

	if strings.TrimSpace(llmmodel.GetAPIKey(modelID)) == "" {
		t.Skip("no API key configured for gpt-5 model")
	}

	mod, err := gocode.NewModule(gocode.MustCwd())
	require.NoError(t, err)

	pkg, err := mod.LoadPackageByRelativeDir("codeai/subagents/updateusage")
	require.NoError(t, err)

	instructions := "Don't make any changes. Instead, describe the tests."
	answer, err := updateusage.UpdateUsage(context.Background(), agent.NewAgentCreator(), mod.AbsolutePath, nil, nil, pkg.AbsolutePath(), toolsets.LimitedPackageAgentTools, instructions)
	if err != nil {
		t.Fatalf("UpdateUsage: %v", err)
	}
	if strings.TrimSpace(answer) == "" {
		t.Fatal("UpdateUsage returned an empty answer")
	}

	t.Logf("UpdateUsage response: %s", answer)
}
