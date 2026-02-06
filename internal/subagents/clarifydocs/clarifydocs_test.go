package clarifydocs_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/prompt"
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

type capturingAgentCreator struct {
	gotSystemPrompt string
}

func (c *capturingAgentCreator) New(model llmmodel.ModelID, systemPrompt string, tools []llmstream.Tool) (*agent.Agent, error) {
	c.gotSystemPrompt = systemPrompt
	return nil, errors.New("stop")
}

func (c *capturingAgentCreator) NewWithDefaultModel(systemPrompt string, tools []llmstream.Tool) (*agent.Agent, error) {
	c.gotSystemPrompt = systemPrompt
	return nil, errors.New("stop")
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

func TestClarifyAPI_SystemPromptIncludesEnvBlock(t *testing.T) {
	wd, err := os.Getwd()
	assert.NoError(t, err)

	targetPath := filepath.Join(wd, "clarifydocs.go")
	sandboxAbsDir := t.TempDir()

	ac := &capturingAgentCreator{}
	_, err = clarifydocs.ClarifyAPI(
		context.Background(),
		ac,
		sandboxAbsDir,
		nil,
		func(sandboxDir string, authorizer authdomain.Authorizer) ([]llmstream.Tool, error) { return nil, nil },
		targetPath,
		"ClarifyAPI",
		"What does ClarifyAPI return?",
	)
	assert.Error(t, err)

	base := prompt.GetFullPrompt()
	assert.True(t, strings.HasPrefix(ac.gotSystemPrompt, base))
	assert.True(t, strings.HasSuffix(ac.gotSystemPrompt, "\n\n<env>\nSandbox directory: "+sandboxAbsDir+"\n</env>\n"))
}
