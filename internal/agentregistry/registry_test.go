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
			Name:       "test",
			AuthPolicy: AuthPolicyDefault,
		}
		assert.NoError(t, def.Validate())
	})

	t.Run("valid package policy", func(t *testing.T) {
		def := Definition{
			Name:       "test",
			AuthPolicy: AuthPolicyPackage,
		}
		assert.NoError(t, def.Validate())
	})

	t.Run("invalid policy", func(t *testing.T) {
		def := Definition{
			Name:       "test",
			AuthPolicy: AuthPolicy("invalid"),
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

type recordingAgentCreator struct {
	base      agent.AgentCreator
	lastAgent *agent.Agent
}

func newRecordingAgentCreator() *recordingAgentCreator {
	return &recordingAgentCreator{base: agent.NewAgentCreator()}
}

func (r *recordingAgentCreator) New(model llmmodel.ModelID, systemPrompt string, tools []llmstream.Tool) (*agent.Agent, error) {
	a, err := r.base.New(model, systemPrompt, tools)
	if err == nil {
		r.lastAgent = a
	}
	return a, err
}

func (r *recordingAgentCreator) NewWithDefaultModel(systemPrompt string, tools []llmstream.Tool) (*agent.Agent, error) {
	a, err := r.base.NewWithDefaultModel(systemPrompt, tools)
	if err == nil {
		r.lastAgent = a
	}
	return a, err
}

func userTurnTexts(turns []llmstream.Turn) []string {
	var texts []string
	for _, turn := range turns {
		if turn.Role == llmstream.RoleUser {
			texts = append(texts, turn.TextContent())
		}
	}
	return texts
}

type stubTool struct {
	name string
}

func (t stubTool) Info() llmstream.ToolInfo {
	return llmstream.ToolInfo{Name: t.name}
}

func (t stubTool) Name() string {
	return t.name
}

func (t stubTool) Run(ctx context.Context, params llmstream.ToolCall) llmstream.ToolResult {
	return llmstream.ToolResult{
		CallID: params.CallID,
		Name:   t.name,
		Type:   params.Type,
	}
}

func TestPreparedAgent_Create(t *testing.T) {
	t.Run("requires prepared agent", func(t *testing.T) {
		var prepared *PreparedAgent
		_, err := prepared.Create(agent.NewAgentCreator())
		assert.ErrorContains(t, err, "prepared agent is required")
	})

	t.Run("requires agent creator", func(t *testing.T) {
		prepared := &PreparedAgent{}
		_, err := prepared.Create(nil)
		assert.ErrorContains(t, err, "AgentCreator is required")
	})

	t.Run("creates idle agent with initial turns and single use", func(t *testing.T) {
		prepared := &PreparedAgent{
			BuildOptions: BuildOptions{
				ToolOptions: toolsetinterface.Options{
					Model: "test-model",
				},
			},
			SystemPrompt: "System Prompt",
			ToolNames:    []string{"tool-a"},
			InitialTurns: []string{"initial-1", "initial-2"},
		}

		creator := newRecordingAgentCreator()
		a, err := prepared.Create(creator)
		require.NoError(t, err)
		require.NotNil(t, a)
		assert.Same(t, a, creator.lastAgent)
		assert.NotEmpty(t, a.SessionID())
		assert.Equal(t, []string{"initial-1", "initial-2"}, userTurnTexts(a.Turns()))
		assert.ErrorIs(t, a.QueueUserMessage("later"), agent.ErrNotRunning)

		_, err = prepared.Create(creator)
		assert.ErrorContains(t, err, "prepared agent already created")
	})
}

func TestRegistry_PrepareAndCreate(t *testing.T) {
	r := NewRegistry()

	require.NoError(t, r.RegisterTool("prepare-static-tool", func(opts toolsetinterface.Options) (llmstream.Tool, error) {
		assert.Equal(t, llmmodel.ModelID("prepared-model"), opts.Model)
		return stubTool{name: "prepare-static-tool"}, nil
	}))
	require.NoError(t, r.RegisterTool("prepare-dynamic-tool", func(opts toolsetinterface.Options) (llmstream.Tool, error) {
		assert.Equal(t, "/caller-sandbox", opts.SandboxDir)
		require.NotNil(t, opts.Authorizer)
		assert.Equal(t, "/override-sandbox", opts.Authorizer.SandboxDir())
		return stubTool{name: "prepare-dynamic-tool"}, nil
	}))
	require.NoError(t, r.RegisterAgent(Definition{
		Name:         "prepared-agent",
		SystemPrompt: "base prompt",
		ToolNames:    []string{"prepare-static-tool"},
		ToolsBuilder: func(opts toolsetinterface.Options) ([]string, error) {
			assert.Equal(t, llmmodel.ModelID("prepared-model"), opts.Model)
			return []string{"prepare-dynamic-tool"}, nil
		},
		SystemPromptBuilder: func(opts BuildOptions) (string, error) {
			assert.Equal(t, "prepared-agent", opts.AgentName)
			assert.Equal(t, []string{"real message"}, opts.Request.Messages)
			return "dynamic prompt", nil
		},
		InitialTurnsBuilder: func(ctx context.Context, opts BuildOptions) ([]string, error) {
			assert.Equal(t, []string{"real message"}, opts.Request.Messages)
			return []string{"initial-context"}, nil
		},
	}))

	req := toolsetinterface.InvokeRequest{
		CallerAuthorizer:   authdomain.NewAutoApproveAuthorizer("/caller-sandbox"),
		CallerSandboxDir:   "/caller-sandbox",
		OverrideAuthorizer: authdomain.NewAutoApproveAuthorizer("/override-sandbox"),
		ToolOptions: toolsetinterface.Options{
			Model:      "prepared-model",
			Authorizer: authdomain.NewAutoApproveAuthorizer("/base-sandbox"),
			SandboxDir: "/base-sandbox",
		},
		Messages: []string{"real message"},
	}

	t.Run("prepare resolves definition without agent creator", func(t *testing.T) {
		prepared, err := r.Prepare(context.Background(), "prepared-agent", req)
		require.NoError(t, err)
		require.NotNil(t, prepared)

		assert.Equal(t, "dynamic prompt", prepared.SystemPrompt)
		assert.Equal(t, []string{"prepare-static-tool", "prepare-dynamic-tool"}, prepared.ToolNames)
		assert.Equal(t, []string{"initial-context"}, prepared.InitialTurns)
		assert.Equal(t, llmmodel.ModelID("prepared-model"), prepared.BuildOptions.ToolOptions.Model)
		assert.Equal(t, "/caller-sandbox", prepared.BuildOptions.ToolOptions.SandboxDir)
		assert.True(t, prepared.BuildOptions.ToolOptions.Authorizer == req.OverrideAuthorizer)
		invoker, ok := prepared.BuildOptions.ToolOptions.AgentInvoker.(*Registry)
		require.True(t, ok)
		assert.Same(t, r, invoker)
		assert.Equal(t, []string{"real message"}, prepared.BuildOptions.Request.Messages)
	})

	t.Run("create returns idle agent and does not apply request messages", func(t *testing.T) {
		creator := newRecordingAgentCreator()
		createReq := req
		createReq.AgentCreator = creator

		a, err := r.Create(context.Background(), "prepared-agent", createReq)
		require.NoError(t, err)
		require.NotNil(t, a)
		assert.Same(t, a, creator.lastAgent)
		assert.Equal(t, []string{"initial-context"}, userTurnTexts(a.Turns()))
		assert.ErrorIs(t, a.QueueUserMessage("later"), agent.ErrNotRunning)
	})
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
			Name:       "pkg-agent",
			AuthPolicy: AuthPolicyPackage,
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
		assert.ErrorContains(t, err, "GoPkgAbsDir is required for AuthPolicyPackage")
	})

	t.Run("package auth policy requires authorizer", func(t *testing.T) {
		require.NoError(t, r.RegisterAgent(Definition{
			Name:       "pkg-agent-needs-authorizer",
			AuthPolicy: AuthPolicyPackage,
		}))

		req := toolsetinterface.InvokeRequest{
			AgentCreator: &mockAgentCreator{},
			ToolOptions: toolsetinterface.Options{
				GoPkgAbsDir: "/base/sandbox/my-pkg",
			},
		}

		_, err := r.Invoke(context.Background(), "pkg-agent-needs-authorizer", req)
		assert.ErrorContains(t, err, "authorizer is required for AuthPolicyPackage")
	})

	t.Run("package auth policy preserves caller sandbox and updates authorizer jail", func(t *testing.T) {
		pkgDef := Definition{
			Name:       "pkg-agent-ok",
			AuthPolicy: AuthPolicyPackage,
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
			CallerSandboxDir: "/base/sandbox",
			ToolOptions: toolsetinterface.Options{
				GoPkgAbsDir: "/base/sandbox/my-pkg",
			},
		}

		_, err := r.Invoke(context.Background(), "pkg-agent-ok-tool", req)
		assert.ErrorContains(t, err, "mock stop")

		assert.Equal(t, "/base/sandbox", capturedOpts.SandboxDir)
		assert.NotNil(t, capturedOpts.Authorizer)
		assert.Equal(t, "/base/sandbox/my-pkg", capturedOpts.Authorizer.SandboxDir())
	})

	t.Run("package auth policy uses tool option authorizer and preserves tool sandbox when caller is absent", func(t *testing.T) {
		require.NoError(t, r.RegisterTool("package-toolopt-capture-tool", func(opts toolsetinterface.Options) (llmstream.Tool, error) {
			assert.Equal(t, "/base/sandbox", opts.SandboxDir)
			require.NotNil(t, opts.Authorizer)
			assert.Equal(t, "/base/sandbox/toolopt-pkg", opts.Authorizer.SandboxDir())
			return nil, nil
		}))
		require.NoError(t, r.RegisterAgent(Definition{
			Name:       "pkg-agent-toolopt-authorizer",
			AuthPolicy: AuthPolicyPackage,
			ToolNames:  []string{"package-toolopt-capture-tool"},
		}))

		req := toolsetinterface.InvokeRequest{
			AgentCreator: &mockAgentCreator{err: errors.New("mock stop")},
			ToolOptions: toolsetinterface.Options{
				SandboxDir:  "/base/sandbox",
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

	t.Run("request messages are forwarded in order after initial turns", func(t *testing.T) {
		require.NoError(t, r.RegisterAgent(Definition{
			Name:         "ordered-messages-agent",
			SystemPrompt: "System Prompt",
			InitialTurnsBuilder: func(ctx context.Context, opts BuildOptions) ([]string, error) {
				return []string{"initial-1", "initial-2"}, nil
			},
		}))

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		creator := newRecordingAgentCreator()
		events, err := r.Invoke(ctx, "ordered-messages-agent", toolsetinterface.InvokeRequest{
			AgentCreator: creator,
			ToolOptions: toolsetinterface.Options{
				Model: "test-model",
			},
			Messages: []string{"message-1", "message-2", "message-3"},
		})
		require.NoError(t, err)
		require.NotNil(t, creator.lastAgent)

		for range events {
		}

		assert.Equal(t, []string{
			"initial-1",
			"initial-2",
			"message-1",
			"message-2",
			"message-3",
		}, userTurnTexts(creator.lastAgent.Turns()))
	})

	t.Run("empty request messages preserve empty single-message behavior", func(t *testing.T) {
		require.NoError(t, r.RegisterAgent(Definition{
			Name:         "empty-messages-agent",
			SystemPrompt: "System Prompt",
		}))

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		creator := newRecordingAgentCreator()
		events, err := r.Invoke(ctx, "empty-messages-agent", toolsetinterface.InvokeRequest{
			AgentCreator: creator,
			ToolOptions: toolsetinterface.Options{
				Model: "test-model",
			},
		})
		require.NoError(t, err)
		require.NotNil(t, creator.lastAgent)

		for range events {
		}

		assert.Equal(t, []string{""}, userTurnTexts(creator.lastAgent.Turns()))
	})
}
