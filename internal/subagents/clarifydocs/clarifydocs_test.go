package clarifydocs_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/subagents/clarifydocs"
	"github.com/codalotl/codalotl/internal/tools/authdomain"

	"github.com/stretchr/testify/assert"
)

type failingAgentCreator struct{}

func (failingAgentCreator) New(model llmmodel.ModelID, systemPrompt string, tools []llmstream.Tool) (*agent.Agent, error) {
	return nil, errors.New("not implemented")
}

func (failingAgentCreator) NewWithDefaultModel(systemPrompt string, tools []llmstream.Tool) (*agent.Agent, error) {
	return nil, errors.New("not implemented")
}

func TestClarifyAPI_AllowsAbsolutePathOutsideSandbox(t *testing.T) {
	sandboxAbsDir := t.TempDir()

	outsideDir := t.TempDir()
	outsidePath := filepath.Join(outsideDir, "target.txt")
	err := os.WriteFile(outsidePath, []byte("hello"), 0644)
	assert.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = clarifydocs.ClarifyAPI(
		ctx,
		failingAgentCreator{},
		sandboxAbsDir,
		nil,
		func(sandboxDir string, authorizer authdomain.Authorizer) ([]llmstream.Tool, error) { return nil, nil },
		outsidePath,
		"SomeIdentifier",
		"SomeQuestion",
	)
	assert.ErrorIs(t, err, context.Canceled)
}
