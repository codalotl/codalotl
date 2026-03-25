package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/mockllm/mockopenai"
	"github.com/codalotl/codalotl/internal/noninteractive"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testCaseConfig struct {
	Prompt      string           `json:"prompt"`
	PackagePath string           `json:"package_path"`
	Expected    []map[string]any `json:"expected"`
}

func TestIntegrationCases(t *testing.T) {
	caseNames := listCaseNames(t)
	for _, caseName := range caseNames {
		t.Run(caseName, func(t *testing.T) {
			runCase(t, caseName)
		})
	}
}

func listCaseNames(t *testing.T) []string {
	t.Helper()

	entries, err := os.ReadDir("testdata")
	require.NoError(t, err)

	caseNames := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		caseNames = append(caseNames, entry.Name())
	}
	sort.Strings(caseNames)
	return caseNames
}

func runCase(t *testing.T, caseName string) {
	t.Helper()

	caseDir := filepath.Join("testdata", caseName)
	cfg := readConfig(t, filepath.Join(caseDir, "config.json"))

	handler, err := mockopenai.NewHandlerFromFile(filepath.Join(caseDir, "http.json"))
	require.NoError(t, err)

	server := httptest.NewServer(handler)
	defer server.Close()

	workDir := t.TempDir()
	copyTreeIfPresent(t, filepath.Join(caseDir, "repo"), workDir)

	modelID := registerMockModel(t, caseName, server.URL)

	var out bytes.Buffer
	err = noninteractive.Exec(cfg.Prompt, noninteractive.Options{
		CWD:         workDir,
		PackagePath: cfg.PackagePath,
		ModelID:     modelID,
		OutputJSON:  true,
		AutoYes:     true,
		Out:         &out,
	})
	require.NoError(t, err)

	actualEvents := parseJSONLines(t, out.Bytes())
	require.NotEmpty(t, actualEvents)
	assertNoTerminalFailure(t, actualEvents)
	assertEventSubsequence(t, cfg.Expected, actualEvents)
	assertExpectedRepo(t, filepath.Join(caseDir, "expected_repo"), workDir)
	require.NoError(t, mockopenai.AssertAllConsumed(handler))
}

func readConfig(t *testing.T, path string) testCaseConfig {
	t.Helper()

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var cfg testCaseConfig
	require.NoError(t, json.Unmarshal(data, &cfg))
	require.NotEmpty(t, strings.TrimSpace(cfg.Prompt))
	require.NotEmpty(t, cfg.Expected)
	return cfg
}

func registerMockModel(t *testing.T, caseName string, baseURL string) llmmodel.ModelID {
	t.Helper()

	suffix := sanitizeIdentifier(caseName)
	modelID := llmmodel.ModelID("integration-" + suffix)
	providerModelID := "mock-model-" + suffix

	err := llmmodel.AddCustomModel(modelID, llmmodel.ProviderIDOpenAI, providerModelID, llmmodel.ModelOverrides{
		APIActualKey:   "test-openai-key",
		APIEndpointURL: baseURL,
	})
	require.NoError(t, err)

	return modelID
}

func sanitizeIdentifier(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			continue
		}
		b.WriteRune('-')
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "case"
	}
	return out
}

func parseJSONLines(t *testing.T, data []byte) []map[string]any {
	t.Helper()

	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	events := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var event map[string]any
		require.NoError(t, json.Unmarshal(line, &event))
		events = append(events, event)
	}
	return events
}

func assertNoTerminalFailure(t *testing.T, actual []map[string]any) {
	t.Helper()

	for _, event := range actual {
		eventType, _ := event["type"].(string)
		if eventType == "error" || eventType == "canceled" {
			formatted, err := json.MarshalIndent(event, "", "  ")
			require.NoError(t, err)
			t.Fatalf("unexpected terminal event:\n%s", formatted)
		}
	}
}

func assertEventSubsequence(t *testing.T, expected []map[string]any, actual []map[string]any) {
	t.Helper()

	actualIdx := 0
	for expectedIdx, want := range expected {
		found := false
		for actualIdx < len(actual) {
			if matchesValue(want, actual[actualIdx]) {
				found = true
				actualIdx++
				break
			}
			actualIdx++
		}
		if found {
			continue
		}

		var rendered []string
		for _, event := range actual {
			pretty, err := json.MarshalIndent(event, "", "  ")
			require.NoError(t, err)
			rendered = append(rendered, string(pretty))
		}

		prettyWant, err := json.MarshalIndent(want, "", "  ")
		require.NoError(t, err)
		t.Fatalf("expected event %d not found:\n%s\n\nactual events:\n%s", expectedIdx, prettyWant, strings.Join(rendered, "\n"))
	}
}

func matchesValue(expected any, actual any) bool {
	if matcher, ok := expected.(map[string]any); ok && isTextMatcher(matcher) {
		return matchesTextMatcher(matcher, actual)
	}

	switch want := expected.(type) {
	case map[string]any:
		got, ok := actual.(map[string]any)
		if !ok {
			return false
		}
		for key, value := range want {
			actualValue, ok := got[key]
			if !ok {
				return false
			}
			if !matchesValue(value, actualValue) {
				return false
			}
		}
		return true
	case []any:
		got, ok := actual.([]any)
		if !ok {
			return false
		}
		if len(want) != len(got) {
			return false
		}
		for i := range want {
			if !matchesValue(want[i], got[i]) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(expected, actual)
	}
}

func isTextMatcher(v map[string]any) bool {
	if _, ok := v["text"]; !ok {
		return false
	}
	if len(v) == 1 {
		return true
	}
	if len(v) == 2 {
		_, ok := v["match"]
		return ok
	}
	return false
}

func matchesTextMatcher(matcher map[string]any, actual any) bool {
	rawText, ok := matcher["text"].(string)
	if !ok {
		return false
	}

	matchType := "exact"
	if rawMatchType, ok := matcher["match"]; ok {
		text, ok := rawMatchType.(string)
		if !ok {
			return false
		}
		matchType = text
	}

	actualText, ok := actualMatchText(actual)
	if !ok {
		return false
	}

	switch matchType {
	case "exact":
		return actualText == rawText
	case "partial":
		return strings.Contains(actualText, rawText) || structuredValueContainsText(actual, rawText)
	default:
		return false
	}
}

func actualMatchText(actual any) (string, bool) {
	if text, ok := actual.(string); ok {
		return text, true
	}

	encoded, err := marshalJSONNoEscape(actual)
	if err != nil {
		return "", false
	}
	return string(encoded), true
}

func marshalJSONNoEscape(value any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

func structuredValueContainsText(actual any, needle string) bool {
	switch value := actual.(type) {
	case string:
		return strings.Contains(value, needle)
	case []any:
		for _, item := range value {
			if structuredValueContainsText(item, needle) {
				return true
			}
		}
	case map[string]any:
		for _, item := range value {
			if structuredValueContainsText(item, needle) {
				return true
			}
		}
	}
	return false
}

func copyTreeIfPresent(t *testing.T, src string, dst string) {
	t.Helper()

	info, err := os.Stat(src)
	if err != nil {
		require.True(t, os.IsNotExist(err))
		return
	}
	require.True(t, info.IsDir())

	err = filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		targetPath := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(targetPath, 0o755)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}
		return os.WriteFile(targetPath, data, 0o644)
	})
	require.NoError(t, err)
}

func assertExpectedRepo(t *testing.T, expectedRoot string, actualRoot string) {
	t.Helper()

	info, err := os.Stat(expectedRoot)
	if err != nil {
		require.True(t, os.IsNotExist(err))
		return
	}
	require.True(t, info.IsDir())

	err = filepath.WalkDir(expectedRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(expectedRoot, path)
		if err != nil {
			return err
		}

		expectedData, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		actualData, err := os.ReadFile(filepath.Join(actualRoot, rel))
		if err != nil {
			return fmt.Errorf("read actual file %q: %w", rel, err)
		}
		assert.Equal(t, string(expectedData), string(actualData))
		return nil
	})
	require.NoError(t, err)
}
