package agentregistry

import (
	"context"
	"errors"
	"testing"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/codalotl/codalotl/internal/tools/toolsetinterface"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistry_RegisterAndLookup(t *testing.T) {
	r := NewRegistry()

	def := Definition{
		Name:         "test-agent",
		SystemPrompt: "Hello",
		ToolNames:    []string{"tool-a"},
	}

	err := r.RegisterAgent(def)
	require.NoError(t, err)
	def.ToolNames[0] = "tool-b"

	lookedUp, ok := r.Lookup("test-agent")
	assert.True(t, ok)
	assert.Equal(t, def.Name, lookedUp.Name)
	assert.Equal(t, def.SystemPrompt, lookedUp.SystemPrompt)
	assert.Equal(t, []string{"tool-a"}, lookedUp.ToolNames)
	lookedUp.ToolNames[0] = "tool-c"

	list := r.List()
	require.Len(t, list, 1)
	assert.Equal(t, def.Name, list[0].Name)
	assert.Equal(t, []string{"tool-a"}, list[0].ToolNames)
	list[0].ToolNames[0] = "tool-d"

	lookedUpAgain, ok := r.Lookup("test-agent")
	require.True(t, ok)
	assert.Equal(t, []string{"tool-a"}, lookedUpAgain.ToolNames)

	_, ok = r.Lookup("missing")
	assert.False(t, ok)

	err = r.RegisterAgent(Definition{})
	assert.Error(t, err)
}

func TestRegistry_ListSorted(t *testing.T) {
	r := NewRegistry()

	require.NoError(t, r.RegisterAgent(Definition{Name: "z-agent"}))
	require.NoError(t, r.RegisterAgent(Definition{Name: "a-agent"}))

	list := r.List()
	require.Len(t, list, 2)
	assert.Equal(t, "a-agent", list[0].Name)
	assert.Equal(t, "z-agent", list[1].Name)
}

func TestRegistry_Tools(t *testing.T) {
	r := NewRegistry()

	dummyToolFunc := func(opts toolsetinterface.Options) (llmstream.Tool, error) {
		return nil, nil
	}

	err := r.RegisterTool("test-tool", dummyToolFunc)
	require.NoError(t, err)

	err = r.RegisterAgent(Definition{
		Name:      "test-agent",
		ToolNames: []string{"test-tool"},
	})
	require.NoError(t, err)

	err = r.ValidateTools()
	assert.NoError(t, err)

	err = r.RegisterAgent(Definition{
		Name:      "test-agent-missing-tool",
		ToolNames: []string{"missing-tool"},
	})
	require.NoError(t, err)

	err = r.ValidateTools()
	assert.ErrorContains(t, err, `agent "test-agent-missing-tool" references unknown tool "missing-tool"`)
}

func TestDefinition_Validate(t *testing.T) {
	t.Run("empty name", func(t *testing.T) {
		def := Definition{}
		assert.Error(t, def.Validate())
	})

	t.Run("valid default policy", func(t *testing.T) {
		def := Definition{
			Name:              "test",
			AuthPackagePolicy: AuthPackagePolicyDefault,
		}
		assert.NoError(t, def.Validate())
	})

	t.Run("valid package policy", func(t *testing.T) {
		def := Definition{
			Name:              "test",
			AuthPackagePolicy: AuthPackagePolicyPackage,
		}
		assert.NoError(t, def.Validate())
	})

	t.Run("invalid policy", func(t *testing.T) {
		def := Definition{
			Name:              "test",
			AuthPackagePolicy: AuthPackagePolicy("invalid"),
		}
		assert.Error(t, def.Validate())
	})
}

type mockAgentCreator struct {
	newCalls            int
	newWithDefaultCalls int
	lastModel           llmmodel.ModelID
	lastSystemPrompt    string
	lastTools           []llmstream.Tool
	err                 error
}

func (m *mockAgentCreator) New(model llmmodel.ModelID, systemPrompt string, tools []llmstream.Tool) (*agent.Agent, error) {
	m.newCalls++
	m.lastModel = model
	m.lastSystemPrompt = systemPrompt
	m.lastTools = tools
	return nil, m.err
}

func (m *mockAgentCreator) NewWithDefaultModel(systemPrompt string, tools []llmstream.Tool) (*agent.Agent, error) {
	m.newWithDefaultCalls++
	m.lastSystemPrompt = systemPrompt
	m.lastTools = tools
	return nil, m.err
}

func TestRegistry_Invoke(t *testing.T) {
	r := NewRegistry()

	require.NoError(t, r.RegisterTool("my-tool", func(opts toolsetinterface.Options) (llmstream.Tool, error) {
		return nil, nil // Return a nil tool for this test, since mockAgentCreator won't use it
	}))

	def := Definition{
		Name:         "test-agent",
		SystemPrompt: "System Prompt",
		ToolNames:    []string{"my-tool"},
	}
	require.NoError(t, r.RegisterAgent(def))

	t.Run("default auth policy", func(t *testing.T) {
		mockCreator := &mockAgentCreator{
			err: errors.New("mock stop"),
		}
		authorizer := authdomain.NewAutoApproveAuthorizer("/tool-sandbox")
		var capturedOpts toolsetinterface.Options

		require.NoError(t, r.RegisterTool("default-capture-tool", func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			capturedOpts = opts
			return nil, nil
		}))
		require.NoError(t, r.RegisterAgent(Definition{
			Name:         "default-capture-agent",
			SystemPrompt: "System Prompt",
			ToolNames:    []string{"default-capture-tool"},
		}))

		req := toolsetinterface.InvokeRequest{
			AgentCreator: mockCreator,
			ToolOptions: toolsetinterface.Options{
				Model:      "my-model",
				Authorizer: authorizer,
				SandboxDir: "/tool-sandbox",
			},
			CallerSandboxDir: "/base/sandbox",
		}

		_, err := r.Invoke(context.Background(), "default-capture-agent", req)
		assert.ErrorContains(t, err, "mock stop")

		assert.Equal(t, 1, mockCreator.newCalls)
		assert.Equal(t, llmmodel.ModelID("my-model"), mockCreator.lastModel)
		assert.Equal(t, "System Prompt", mockCreator.lastSystemPrompt)
		assert.Len(t, mockCreator.lastTools, 1)
		assert.True(t, capturedOpts.Authorizer == authorizer)
		assert.Equal(t, "/base/sandbox", capturedOpts.SandboxDir)
		invoker, ok := capturedOpts.AgentInvoker.(*Registry)
		require.True(t, ok)
		assert.Same(t, r, invoker)
	})

	t.Run("default auth policy override wins", func(t *testing.T) {
		mockCreator := &mockAgentCreator{err: errors.New("mock stop")}
		baseAuthorizer := authdomain.NewAutoApproveAuthorizer("/tool-sandbox")
		callerAuthorizer := authdomain.NewAutoApproveAuthorizer("/caller-sandbox")
		overrideAuthorizer := authdomain.NewAutoApproveAuthorizer("/override-sandbox")
		var capturedOpts toolsetinterface.Options

		require.NoError(t, r.RegisterTool("override-capture-tool", func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			capturedOpts = opts
			return nil, nil
		}))
		require.NoError(t, r.RegisterAgent(Definition{
			Name:      "override-capture-agent",
			ToolNames: []string{"override-capture-tool"},
		}))

		req := toolsetinterface.InvokeRequest{
			AgentCreator:       mockCreator,
			CallerAuthorizer:   callerAuthorizer,
			CallerSandboxDir:   "/caller-sandbox",
			OverrideAuthorizer: overrideAuthorizer,
			OverrideSandboxDir: "/override-sandbox",
			ToolOptions: toolsetinterface.Options{
				Authorizer: baseAuthorizer,
				SandboxDir: "/tool-sandbox",
			},
		}

		_, err := r.Invoke(context.Background(), "override-capture-agent", req)
		assert.ErrorContains(t, err, "mock stop")
		assert.True(t, capturedOpts.Authorizer == overrideAuthorizer)
		assert.Equal(t, "/override-sandbox", capturedOpts.SandboxDir)
	})

	t.Run("package auth policy requires pkg dir", func(t *testing.T) {
		pkgDef := Definition{
			Name:              "pkg-agent",
			AuthPackagePolicy: AuthPackagePolicyPackage,
		}
		require.NoError(t, r.RegisterAgent(pkgDef))

		mockCreator := &mockAgentCreator{}

		req := toolsetinterface.InvokeRequest{
			AgentCreator: mockCreator,
			ToolOptions: toolsetinterface.Options{
				GoPkgAbsDir: "",
			},
		}

		_, err := r.Invoke(context.Background(), "pkg-agent", req)
		assert.ErrorContains(t, err, "GoPkgAbsDir is required for AuthPackagePolicyPackage")
	})

	t.Run("package auth policy requires authorizer", func(t *testing.T) {
		require.NoError(t, r.RegisterAgent(Definition{
			Name:              "pkg-agent-needs-authorizer",
			AuthPackagePolicy: AuthPackagePolicyPackage,
		}))

		req := toolsetinterface.InvokeRequest{
			AgentCreator: &mockAgentCreator{},
			ToolOptions: toolsetinterface.Options{
				GoPkgAbsDir: "/base/sandbox/my-pkg",
			},
		}

		_, err := r.Invoke(context.Background(), "pkg-agent-needs-authorizer", req)
		assert.ErrorContains(t, err, "authorizer is required for AuthPackagePolicyPackage")
	})

	t.Run("package auth policy success updates sandbox", func(t *testing.T) {
		pkgDef := Definition{
			Name:              "pkg-agent-ok",
			AuthPackagePolicy: AuthPackagePolicyPackage,
		}
		require.NoError(t, r.RegisterAgent(pkgDef))

		mockCreator := &mockAgentCreator{err: errors.New("mock stop")}
		authorizer := authdomain.NewAutoApproveAuthorizer("/base/sandbox")

		var capturedOpts toolsetinterface.Options
		require.NoError(t, r.RegisterTool("capture-opts-tool", func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			capturedOpts = opts
			return nil, nil
		}))

		pkgDefWithTool := pkgDef
		pkgDefWithTool.Name = "pkg-agent-ok-tool"
		pkgDefWithTool.ToolNames = []string{"capture-opts-tool"}
		require.NoError(t, r.RegisterAgent(pkgDefWithTool))

		req := toolsetinterface.InvokeRequest{
			AgentCreator:     mockCreator,
			CallerAuthorizer: authorizer,
			ToolOptions: toolsetinterface.Options{
				GoPkgAbsDir: "/base/sandbox/my-pkg",
			},
		}

		_, err := r.Invoke(context.Background(), "pkg-agent-ok-tool", req)
		assert.ErrorContains(t, err, "mock stop")

		assert.Equal(t, "/base/sandbox/my-pkg", capturedOpts.SandboxDir)
		assert.NotNil(t, capturedOpts.Authorizer)
		assert.Equal(t, "/base/sandbox/my-pkg", capturedOpts.Authorizer.SandboxDir())
	})

	t.Run("package auth policy uses tool option authorizer when caller is absent", func(t *testing.T) {
		require.NoError(t, r.RegisterTool("package-toolopt-capture-tool", func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			assert.Equal(t, "/base/sandbox/toolopt-pkg", opts.SandboxDir)
			require.NotNil(t, opts.Authorizer)
			assert.Equal(t, "/base/sandbox/toolopt-pkg", opts.Authorizer.SandboxDir())
			return nil, nil
		}))
		require.NoError(t, r.RegisterAgent(Definition{
			Name:              "pkg-agent-toolopt-authorizer",
			AuthPackagePolicy: AuthPackagePolicyPackage,
			ToolNames:         []string{"package-toolopt-capture-tool"},
		}))

		req := toolsetinterface.InvokeRequest{
			AgentCreator: &mockAgentCreator{err: errors.New("mock stop")},
			ToolOptions: toolsetinterface.Options{
				Authorizer:  authdomain.NewAutoApproveAuthorizer("/base/sandbox"),
				GoPkgAbsDir: "/base/sandbox/toolopt-pkg",
			},
		}

		_, err := r.Invoke(context.Background(), "pkg-agent-toolopt-authorizer", req)
		assert.ErrorContains(t, err, "mock stop")
	})

	t.Run("system prompt builder overrides", func(t *testing.T) {
		bDef := Definition{
			Name: "builder-agent",
			SystemPromptBuilder: func(opts BuildOptions) (string, error) {
				return "Dynamic " + opts.AgentName, nil
			},
		}
		require.NoError(t, r.RegisterAgent(bDef))

		mockCreator := &mockAgentCreator{err: errors.New("mock stop")}

		req := toolsetinterface.InvokeRequest{
			AgentCreator: mockCreator,
		}

		_, err := r.Invoke(context.Background(), "builder-agent", req)
		assert.ErrorContains(t, err, "mock stop")

		assert.Equal(t, "Dynamic builder-agent", mockCreator.lastSystemPrompt)
	})

	t.Run("tools builder appends dynamic tools", func(t *testing.T) {
		var constructed []string
		require.NoError(t, r.RegisterTool("tools-builder-static-tool", func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			constructed = append(constructed, "tools-builder-static-tool")
			return nil, nil
		}))
		require.NoError(t, r.RegisterTool("tools-builder-dynamic-tool", func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			constructed = append(constructed, "tools-builder-dynamic-tool")
			return nil, nil
		}))
		require.NoError(t, r.RegisterAgent(Definition{
			Name:      "tools-builder-agent",
			ToolNames: []string{"tools-builder-static-tool"},
			ToolsBuilder: func(opts toolsetinterface.Options) ([]string, error) {
				assert.Equal(t, llmmodel.ModelID("dynamic-model"), opts.Model)
				return []string{"tools-builder-dynamic-tool"}, nil
			},
		}))

		mockCreator := &mockAgentCreator{err: errors.New("mock stop")}

		_, err := r.Invoke(context.Background(), "tools-builder-agent", toolsetinterface.InvokeRequest{
			AgentCreator: mockCreator,
			ToolOptions: toolsetinterface.Options{
				Model: "dynamic-model",
			},
		})
		assert.ErrorContains(t, err, "mock stop")
		assert.Equal(t, []string{"tools-builder-static-tool", "tools-builder-dynamic-tool"}, constructed)
		assert.Len(t, mockCreator.lastTools, 2)
	})

	t.Run("tools builder error stops invoke", func(t *testing.T) {
		require.NoError(t, r.RegisterAgent(Definition{
			Name: "tools-builder-error-agent",
			ToolsBuilder: func(opts toolsetinterface.Options) ([]string, error) {
				return nil, errors.New("builder boom")
			},
		}))

		mockCreator := &mockAgentCreator{}

		_, err := r.Invoke(context.Background(), "tools-builder-error-agent", toolsetinterface.InvokeRequest{
			AgentCreator: mockCreator,
		})
		assert.ErrorContains(t, err, "failed to build tool names: builder boom")
		assert.Zero(t, mockCreator.newCalls)
		assert.Zero(t, mockCreator.newWithDefaultCalls)
	})
}
