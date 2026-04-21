package integration

import (
	"bytes"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codalotl/codalotl/internal/lints"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/mockllm/mockopenai"
	"github.com/codalotl/codalotl/internal/noninteractive"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubagentAssistantTextDoesNotExposeFinalizingField(t *testing.T) {
	events := runCaseDirActualEvents(t, filepath.Join(casesRoot, "pm-update_usage"))

	for _, event := range events {
		_, ok := event["finalizing"]
		assert.False(t, ok)
	}
}

func TestSubagentNonFinalAssistantTextAppearsImmediately(t *testing.T) {
	events := runCaseDirActualEvents(t, filepath.Join(casesRoot, "pm-update_usage"))

	assistantIdx := findEventIndex(events, func(event map[string]any) bool {
		return assistantTextEventContains(event, 1, "updating the `inventory` package call site")
	})
	require.NotEqual(t, -1, assistantIdx)

	toolCallIdx := findEventIndex(events, func(event map[string]any) bool {
		return toolCallEventContains(event, 1, "read_file", "inventory/reservation.go")
	})
	require.NotEqual(t, -1, toolCallIdx)

	assert.Less(t, assistantIdx, toolCallIdx)
}

func TestSubagentFinalAssistantTextRemainsVisibleWithoutPresenter(t *testing.T) {
	events := runCaseDirActualEvents(t, filepath.Join(casesRoot, "pm-update_usage"))

	finalAssistantIdx := findEventIndex(events, func(event map[string]any) bool {
		return assistantTextEventContains(event, 1, "`.HasTag(...)` call sites in `inventory`")
	})

	assert.NotEqual(t, -1, finalAssistantIdx)
}

func TestSubagentFinalAssistantTextIsSuppressedWhenPresenterHandlesIt(t *testing.T) {
	events := runCaseDirActualEvents(t, filepath.Join(casesRoot, "pm-clarify-stdlib"))

	subagentAssistantIdx := findEventIndex(events, func(event map[string]any) bool {
		return assistantTextEventContains(event, 1, "With more than one `%w`, `fmt.Errorf` returns an error whose `Unwrap() []error` returns multiple wrapped errors")
	})
	assert.Equal(t, -1, subagentAssistantIdx)

	toolCompleteIdx := findEventIndex(events, func(event map[string]any) bool {
		return toolCompleteEventContains(event, 0, "clarify_public_api", "returns an error whose `Unwrap() []error` returns multiple wrapped errors")
	})
	assert.NotEqual(t, -1, toolCompleteIdx)
}

func runCaseDirActualEvents(t *testing.T, caseDir string) []map[string]any {
	t.Helper()

	cfg, err := readConfig(filepath.Join(caseDir, "config.json"))
	require.NoError(t, err)

	workDir := t.TempDir()

	sourceRepoDir, err := repoDirForCase(caseDir)
	require.NoError(t, err)
	require.NoError(t, copyTree(sourceRepoDir, workDir))

	httpFixturePath := filepath.Join(caseDir, "http.json")
	httpFixtureCfg, err := readHTTPFixtureConfig(httpFixturePath)
	require.NoError(t, err)

	httpFixtureData, err := marshalHTTPFixtureData(httpFixtureCfg, []string{workDir})
	require.NoError(t, err)

	handler, err := mockopenai.NewHandler(httpFixtureData)
	require.NoError(t, err)

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	modelID := registerTestMockModel(t, filepath.Base(caseDir), server.URL)

	lintSteps, err := lints.ResolveSteps(&cfg.Lints, cfg.ReflowWidth)
	require.NoError(t, err)

	var out bytes.Buffer
	err = noninteractive.Exec(denormalizeConfigPromptText(cfg.Prompt, []string{workDir}), noninteractive.Options{
		CWD:         workDir,
		PackagePath: cfg.PackagePath,
		ModelID:     modelID,
		LintSteps:   lintSteps,
		ReflowWidth: cfg.ReflowWidth,
		OutputJSON:  true,
		AutoYes:     true,
		Out:         &out,
	})
	require.NoError(t, err)

	actualEvents, err := parseJSONLines(out.Bytes())
	require.NoError(t, err)
	require.NotEmpty(t, actualEvents)
	require.NoError(t, assertNoTerminalFailure(actualEvents))
	require.NoError(t, assertExpectedRepo(filepath.Join(caseDir, "expected_repo"), sourceRepoDir, workDir))
	require.NoError(t, mockopenai.AssertAllConsumed(handler))

	return actualEvents
}

func registerTestMockModel(t *testing.T, caseName string, baseURL string) llmmodel.ModelID {
	t.Helper()

	caseSuffix := sanitizeIdentifier(caseName)
	modelID := llmmodel.ModelID("integration-" + caseSuffix + "-" + sanitizeIdentifier(t.Name()))

	err := llmmodel.AddCustomModel(modelID, llmmodel.ProviderIDOpenAI, "mock-model-"+caseSuffix, llmmodel.ModelOverrides{
		APIActualKey:   "test-openai-key",
		APIEndpointURL: baseURL,
	})
	require.NoError(t, err)

	return modelID
}

func findEventIndex(events []map[string]any, predicate func(map[string]any) bool) int {
	for i, event := range events {
		if predicate(event) {
			return i
		}
	}
	return -1
}

func assistantTextEventContains(event map[string]any, depth int, fragment string) bool {
	if event["type"] != "assistant_text" {
		return false
	}

	agent, _ := event["agent"].(map[string]any)
	actualDepth, ok := eventAgentDepth(agent)
	if !ok || actualDepth != depth {
		return false
	}

	content, _ := event["content"].(string)
	return strings.Contains(content, fragment)
}

func toolCallEventContains(event map[string]any, depth int, toolName string, fragment string) bool {
	if event["type"] != "tool_call" {
		return false
	}

	agent, _ := event["agent"].(map[string]any)
	actualDepth, ok := eventAgentDepth(agent)
	if !ok || actualDepth != depth {
		return false
	}

	tool, _ := event["tool"].(map[string]any)
	if tool["name"] != toolName {
		return false
	}

	input, _ := tool["input"].(string)
	return strings.Contains(input, fragment)
}

func toolCompleteEventContains(event map[string]any, depth int, toolName string, fragment string) bool {
	if event["type"] != "tool_complete" {
		return false
	}

	agent, _ := event["agent"].(map[string]any)
	actualDepth, ok := eventAgentDepth(agent)
	if !ok || actualDepth != depth {
		return false
	}

	tool, _ := event["tool"].(map[string]any)
	if tool["name"] != toolName {
		return false
	}

	result, _ := event["result"].(map[string]any)
	output, _ := result["output"].(string)
	return strings.Contains(output, fragment)
}
