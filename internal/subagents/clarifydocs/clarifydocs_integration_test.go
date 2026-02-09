package clarifydocs_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/subagents/clarifydocs"
	"github.com/codalotl/codalotl/internal/tools/coretools"
	"github.com/codalotl/codalotl/internal/tools/toolsetinterface"

	"github.com/stretchr/testify/assert"
)

func TestClarifyAPIIntegration(t *testing.T) {
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

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working dir: %v", err)
	}

	sandboxAbsDir := wd
	targetPath, err := filepath.Abs(filepath.Join(".", "clarifydocs.go"))
	assert.NoError(t, err)

	question := "What does the ClarifyAPI function return when it successfully answers a question?"
	simpleReadOnlyTools := func(opts toolsetinterface.Options) ([]llmstream.Tool, error) {
		return []llmstream.Tool{
			coretools.NewLsTool(opts.Authorizer),
			coretools.NewReadFileTool(opts.Authorizer),
		}, nil
	}

	answer, err := clarifydocs.ClarifyAPI(context.Background(), agent.NewAgentCreator(), sandboxAbsDir, nil, simpleReadOnlyTools, targetPath, "ClarifyAPI", question)
	if err != nil {
		t.Fatalf("ClarifyAPI: %v", err)
	}
	if strings.TrimSpace(answer) == "" {
		t.Fatal("ClarifyAPI returned an empty answer")
	}

	// t.Logf("ClarifyAPI response: %s", answer)
}
