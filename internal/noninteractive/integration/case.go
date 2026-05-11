package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"

	"github.com/codalotl/codalotl/internal/lints"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/mockllm/mockopenai"
	"github.com/codalotl/codalotl/internal/noninteractive"
)

const (
	casesRoot       = "testdata/cases"
	fixtureRepoRoot = "testdata/repo"
)

type testCaseConfig struct {
	Prompt            string                   `json:"prompt"`
	PackagePath       string                   `json:"package_path,omitempty"`
	ReflowWidth       int                      `json:"reflowwidth,omitempty"`
	Lints             lints.Lints              `json:"lints,omitempty"`
	Expected          []map[string]any         `json:"expected"`
	ExpectedRepoFiles []expectedRepoFileConfig `json:"expected_repo_files,omitempty"`
}

type expectedRepoFileConfig struct {
	Path  string         `json:"path"`
	Match map[string]any `json:"match"`
}

var runNoninteractiveExec = noninteractive.Exec

func ListCaseNames(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read integration cases dir: %w", err)
	}

	caseNames := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		caseNames = append(caseNames, entry.Name())
	}
	sort.Strings(caseNames)
	return caseNames, nil
}

func RunCaseDir(caseDir string) error {
	cfg, err := readConfig(filepath.Join(caseDir, "config.json"))
	if err != nil {
		return err
	}

	workDir, err := os.MkdirTemp("", "codalotl-integration-case-")
	if err != nil {
		return fmt.Errorf("create temp work dir: %w", err)
	}
	defer os.RemoveAll(workDir)

	sourceRepoDir, err := repoDirForCase(caseDir)
	if err != nil {
		return err
	}
	if err := copyTree(sourceRepoDir, workDir); err != nil {
		return fmt.Errorf("copy case repo: %w", err)
	}

	httpFixturePath := filepath.Join(caseDir, "http.json")
	httpFixtureCfg, err := readHTTPFixtureConfig(httpFixturePath)
	if err != nil {
		return err
	}

	httpFixtureData, err := marshalHTTPFixtureData(httpFixtureCfg, []string{workDir})
	if err != nil {
		return err
	}

	handler, err := mockopenai.NewHandler(httpFixtureData)
	if err != nil {
		return fmt.Errorf("load mock OpenAI handler: %w", err)
	}

	server := httptest.NewServer(handler)
	defer server.Close()

	modelID, err := registerMockModel(filepath.Base(caseDir), server.URL)
	if err != nil {
		return fmt.Errorf("register mock model: %w", err)
	}

	lintSteps, err := lints.ResolveSteps(&cfg.Lints, cfg.ReflowWidth)
	if err != nil {
		return fmt.Errorf("resolve integration lint steps: %w", err)
	}

	var out bytes.Buffer
	err = runNoninteractiveExec(denormalizeConfigPromptText(cfg.Prompt, []string{workDir}), noninteractive.Options{
		CWD:         workDir,
		PackagePath: cfg.PackagePath,
		ModelID:     modelID,
		LintSteps:   lintSteps,
		OutputJSON:  true,
		AutoYes:     true,
		Out:         &out,
	})
	if err != nil {
		err = augmentReplayMockOpenAIError(err, handler, httpFixtureCfg, []string{workDir})
		return fmt.Errorf("run noninteractive exec: %w", err)
	}

	actualEvents, err := parseJSONLines(out.Bytes())
	if err != nil {
		return err
	}
	if len(actualEvents) == 0 {
		return fmt.Errorf("expected at least one JSON event")
	}
	if err := assertNoTerminalFailure(actualEvents); err != nil {
		return err
	}
	if err := assertEventSubsequence(cfg.Expected, actualEvents, []string{workDir}); err != nil {
		return err
	}
	if err := assertExpectedRepo(filepath.Join(caseDir, "expected_repo"), sourceRepoDir, workDir); err != nil {
		return err
	}
	if err := assertExpectedRepoFileConfigs(cfg.ExpectedRepoFiles, sourceRepoDir, workDir); err != nil {
		return err
	}
	if err := mockopenai.AssertAllConsumed(handler); err != nil {
		return fmt.Errorf("assert all mock responses consumed: %w", err)
	}
	return nil
}

func repoDirForCase(caseDir string) (string, error) {
	caseRepoDir := filepath.Join(caseDir, "repo")
	if _, err := os.Stat(caseRepoDir); err == nil {
		return caseRepoDir, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("stat case repo dir: %w", err)
	}
	fixturePath, err := fixtureRepoPath()
	if err != nil {
		return "", err
	}
	return fixturePath, nil
}

func fixtureRepoPath() (string, error) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("resolve integration package path")
	}
	return filepath.Join(filepath.Dir(filename), fixtureRepoRoot), nil
}

func isFixtureRepoPath(repoPath string) (bool, error) {
	fixturePath, err := fixtureRepoPath()
	if err != nil {
		return false, err
	}

	normalizedRepoPath, err := normalizeExistingPath(repoPath)
	if err != nil {
		return false, fmt.Errorf("normalize repo path: %w", err)
	}
	normalizedFixturePath, err := normalizeExistingPath(fixturePath)
	if err != nil {
		return false, fmt.Errorf("normalize fixture repo path: %w", err)
	}
	return normalizedRepoPath == normalizedFixturePath, nil
}

func normalizeExistingPath(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path: %w", err)
	}

	resolvedPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return "", fmt.Errorf("evaluate symlinks: %w", err)
	}
	return resolvedPath, nil
}

func readConfig(path string) (testCaseConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return testCaseConfig{}, fmt.Errorf("read integration config: %w", err)
	}

	var cfg testCaseConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return testCaseConfig{}, fmt.Errorf("parse integration config: %w", err)
	}
	if strings.TrimSpace(cfg.Prompt) == "" {
		return testCaseConfig{}, fmt.Errorf("integration config prompt must not be empty")
	}
	if len(cfg.Expected) == 0 {
		return testCaseConfig{}, fmt.Errorf("integration config expected must not be empty")
	}
	return cfg, nil
}

func readHTTPFixtureConfig(path string) (httpFixtureConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return httpFixtureConfig{}, fmt.Errorf("read integration http fixture: %w", err)
	}

	var cfg httpFixtureConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return httpFixtureConfig{}, fmt.Errorf("parse integration http fixture: %w", err)
	}
	return cfg, nil
}

func loadHTTPFixtureData(path string, roots []string) ([]byte, error) {
	cfg, err := readHTTPFixtureConfig(path)
	if err != nil {
		return nil, err
	}
	return marshalHTTPFixtureData(cfg, roots)
}

func marshalHTTPFixtureData(cfg httpFixtureConfig, roots []string) ([]byte, error) {
	denormalized := httpFixtureConfig{
		Responses: make([]httpFixtureResponse, 0, len(cfg.Responses)),
	}
	for _, response := range cfg.Responses {
		denormalized.Responses = append(denormalized.Responses, httpFixtureResponse{
			Name:     response.Name,
			Consume:  response.Consume,
			Request:  cloneJSONObjectFromValue(denormalizeHTTPJSONAbsolutePaths(response.Request, roots)),
			Response: cloneJSONObjectFromValue(denormalizeHTTPJSONAbsolutePaths(response.Response, roots)),
		})
	}

	normalizedData, err := marshalPrettyJSON(denormalized)
	if err != nil {
		return nil, fmt.Errorf("marshal integration http fixture: %w", err)
	}
	return normalizedData, nil
}

func augmentReplayMockOpenAIError(runErr error, handler http.Handler, fixture httpFixtureConfig, roots []string) error {
	debugInfo, err := mockopenai.DebugInfo(handler)
	if err != nil || debugInfo.LastUnmatchedRequest == nil {
		return runErr
	}

	requestSent, err := buildReplayDebugHTTPFixtureRequest(debugInfo.LastUnmatchedRequest, roots)
	if err != nil {
		return runErr
	}

	requestSentJSON, err := marshalPrettyJSON(requestSent)
	if err != nil {
		return runErr
	}

	extra := "\n\npruned request sent to mockopenai:\n" + string(requestSentJSON)

	if debugInfo.NextUnconsumedConsumedIndex >= 0 && debugInfo.NextUnconsumedConsumedIndex < len(fixture.Responses) {
		nextRequestJSON, err := marshalPrettyJSON(fixture.Responses[debugInfo.NextUnconsumedConsumedIndex].Request)
		if err == nil {
			label := "next non-consumed request in http.json"
			if name := fixture.Responses[debugInfo.NextUnconsumedConsumedIndex].Name; name != "" {
				label += " (" + name + ")"
			}
			extra += "\n" + label + ":\n" + string(nextRequestJSON)
		}
	}

	return fmt.Errorf("%w%s", runErr, extra)
}

func registerMockModel(caseName string, baseURL string) (llmmodel.ModelID, error) {
	suffix := sanitizeIdentifier(caseName)
	modelID := llmmodel.ModelID("integration-" + suffix)
	providerModelID := "mock-model-" + suffix

	err := llmmodel.AddCustomModel(modelID, llmmodel.ProviderIDOpenAI, providerModelID, llmmodel.ModelOverrides{
		APIActualKey:   "test-openai-key",
		APIEndpointURL: baseURL,
	})
	if err != nil {
		return "", err
	}

	return modelID, nil
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

func parseJSONLines(data []byte) ([]map[string]any, error) {
	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	events := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal(line, &event); err != nil {
			return nil, fmt.Errorf("parse NDJSON line: %w", err)
		}
		events = append(events, event)
	}
	return insertImplicitStartSubagentEvents(events), nil
}

func insertImplicitStartSubagentEvents(events []map[string]any) []map[string]any {
	for _, event := range events {
		eventType, _ := event["type"].(string)
		if eventType == "start_subagent" {
			return events
		}
	}

	out := make([]map[string]any, 0, len(events))
	seenAgentIDs := make(map[string]struct{})
	for _, event := range events {
		if shouldInsertImplicitStartSubagent(event, seenAgentIDs) {
			out = append(out, map[string]any{"type": "start_subagent"})
		}
		out = append(out, event)
	}
	return out
}

func shouldInsertImplicitStartSubagent(event map[string]any, seenAgentIDs map[string]struct{}) bool {
	agentValue, ok := event["agent"].(map[string]any)
	if !ok {
		return false
	}

	depth, ok := eventAgentDepth(agentValue)
	if !ok || depth <= 0 {
		return false
	}

	agentID, _ := agentValue["id"].(string)
	if agentID == "" {
		return false
	}
	if _, ok := seenAgentIDs[agentID]; ok {
		return false
	}

	seenAgentIDs[agentID] = struct{}{}
	return true
}

func eventAgentDepth(agentValue map[string]any) (int, bool) {
	switch depth := agentValue["depth"].(type) {
	case int:
		return depth, true
	case int32:
		return int(depth), true
	case int64:
		return int(depth), true
	case float64:
		return int(depth), true
	default:
		return 0, false
	}
}

func assertNoTerminalFailure(actual []map[string]any) error {
	for _, event := range actual {
		eventType, _ := event["type"].(string)
		if eventType != "error" && eventType != "canceled" {
			continue
		}
		formatted, err := json.MarshalIndent(event, "", "  ")
		if err != nil {
			return fmt.Errorf("format terminal event: %w", err)
		}
		return fmt.Errorf("unexpected terminal event:\n%s", formatted)
	}
	return nil
}

func assertEventSubsequence(expected []map[string]any, actual []map[string]any, roots []string) error {
	actualIdx := 0
	for expectedIdx, want := range expected {
		found := false
		for actualIdx < len(actual) {
			if matchesValue(want, actual[actualIdx], roots) {
				found = true
				actualIdx++
				break
			}
			actualIdx++
		}
		if found {
			continue
		}

		rendered := make([]string, 0, len(actual))
		for _, event := range actual {
			pretty, err := json.MarshalIndent(event, "", "  ")
			if err != nil {
				return fmt.Errorf("format actual event: %w", err)
			}
			rendered = append(rendered, string(pretty))
		}

		prettyWant, err := json.MarshalIndent(want, "", "  ")
		if err != nil {
			return fmt.Errorf("format expected event: %w", err)
		}
		return fmt.Errorf("expected event %d not found:\n%s\n\nactual events:\n%s", expectedIdx, prettyWant, strings.Join(rendered, "\n"))
	}
	return nil
}

func matchesValue(expected any, actual any, roots []string) bool {
	if matcher, ok := expected.(map[string]any); ok && isTextMatcher(matcher) {
		return matchesTextMatcher(matcher, actual, roots)
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
			if !matchesValue(value, actualValue, roots) {
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
			if !matchesValue(want[i], got[i], roots) {
				return false
			}
		}
		return true
	case string:
		got, ok := actual.(string)
		if !ok {
			return false
		}
		return normalizeAbsolutePathText(want, roots) == normalizeAbsolutePathText(got, roots)
	default:
		return reflect.DeepEqual(expected, actual)
	}
}

func isTextMatcher(v map[string]any) bool {
	if _, ok := v["text"]; !ok {
		if _, ok := v["texts"]; !ok {
			return false
		}
	}
	if _, ok := v["text"]; ok {
		if _, ok := v["texts"]; ok {
			return false
		}
	}
	if _, ok := v["texts"]; ok {
		if len(v) == 1 {
			return true
		}
		if len(v) == 2 {
			_, ok := v["match"]
			return ok
		}
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

func matchesTextMatcher(matcher map[string]any, actual any, roots []string) bool {
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
	normalizedActual := normalizeJSONAbsolutePaths(actual, roots)
	actualText = normalizeAbsolutePathText(actualText, roots)

	if rawTexts, ok := matcher["texts"]; ok {
		var texts []string
		switch v := rawTexts.(type) {
		case []any:
			texts = make([]string, 0, len(v))
			for _, rawText := range v {
				text, ok := rawText.(string)
				if !ok {
					return false
				}
				texts = append(texts, text)
			}
		case []string:
			texts = v
		default:
			return false
		}
		if matchType != "partial" {
			return false
		}
		normalizedTexts := make([]string, 0, len(texts))
		for _, text := range texts {
			normalizedTexts = append(normalizedTexts, normalizeAbsolutePathText(text, roots))
		}
		if containsTextsInOrder(actualText, normalizedTexts) {
			return true
		}
		if len(normalizedTexts) == 1 {
			return structuredValueContainsText(normalizedActual, normalizedTexts[0])
		}
		return false
	}

	rawText, ok := matcher["text"].(string)
	if !ok {
		return false
	}
	rawText = normalizeAbsolutePathText(rawText, roots)

	switch matchType {
	case "exact":
		return actualText == rawText
	case "partial":
		return strings.Contains(actualText, rawText) || structuredValueContainsText(normalizedActual, rawText)
	default:
		return false
	}
}

func containsTextsInOrder(actualText string, texts []string) bool {
	searchFrom := 0
	for _, text := range texts {
		idx := strings.Index(actualText[searchFrom:], text)
		if idx < 0 {
			return false
		}
		searchFrom += idx + len(text)
	}
	return true
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

func copyTree(src string, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat source dir: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("source %q is not a directory", src)
	}

	return filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
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
}

func assertExpectedRepo(expectedRoot string, originalRoot string, actualRoot string) error {
	expectedFiles, err := listFilesIfPresent(expectedRoot)
	if err != nil {
		return err
	}
	if len(expectedFiles) == 0 {
		return nil
	}

	sort.Strings(expectedFiles)

	actualChangedFiles, err := changedOrCreatedFiles(originalRoot, actualRoot)
	if err != nil {
		return err
	}
	sort.Strings(actualChangedFiles)
	if !reflect.DeepEqual(expectedFiles, actualChangedFiles) {
		return fmt.Errorf("expected changed files %v, got %v", expectedFiles, actualChangedFiles)
	}

	for _, rel := range expectedFiles {
		expectedData, err := os.ReadFile(filepath.Join(expectedRoot, rel))
		if err != nil {
			return fmt.Errorf("read expected file %q: %w", rel, err)
		}

		actualData, err := os.ReadFile(filepath.Join(actualRoot, rel))
		if err != nil {
			return fmt.Errorf("read actual file %q: %w", rel, err)
		}

		if !bytes.Equal(expectedData, actualData) {
			return fmt.Errorf("contents mismatch for %q", rel)
		}
	}
	return nil
}

func assertExpectedRepoFileConfigs(expected []expectedRepoFileConfig, originalRoot string, actualRoot string) error {
	if len(expected) == 0 {
		return nil
	}

	type expectedRepoFile struct {
		path  string
		match map[string]any
	}

	normalizedExpected := make([]expectedRepoFile, 0, len(expected))
	expectedFiles := make([]string, 0, len(expected))
	for _, file := range expected {
		rel := filepath.FromSlash(file.Path)
		if rel == "" {
			return fmt.Errorf("expected repo file path must not be empty")
		}
		if filepath.IsAbs(rel) {
			return fmt.Errorf("expected repo file path must be relative: %q", rel)
		}
		if file.Match == nil {
			return fmt.Errorf("expected repo file %q match must not be empty", rel)
		}
		normalizedExpected = append(normalizedExpected, expectedRepoFile{path: rel, match: file.Match})
		expectedFiles = append(expectedFiles, rel)
	}
	sort.Strings(expectedFiles)

	actualChangedFiles, err := changedOrCreatedFiles(originalRoot, actualRoot)
	if err != nil {
		return err
	}
	sort.Strings(actualChangedFiles)
	if !reflect.DeepEqual(expectedFiles, actualChangedFiles) {
		return fmt.Errorf("expected changed files %v, got %v", expectedFiles, actualChangedFiles)
	}

	for _, file := range normalizedExpected {
		actualData, err := os.ReadFile(filepath.Join(actualRoot, file.path))
		if err != nil {
			return fmt.Errorf("read actual file %q: %w", file.path, err)
		}
		if !matchesValue(file.match, string(actualData), []string{actualRoot}) {
			return fmt.Errorf("contents mismatch for %q", file.path)
		}
	}
	return nil
}

func listFilesIfPresent(root string) ([]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat dir %q: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%q is not a directory", root)
	}

	files := make([]string, 0)
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

func changedOrCreatedFiles(originalRoot string, actualRoot string) ([]string, error) {
	actualFiles, err := listFilesIfPresent(actualRoot)
	if err != nil {
		return nil, err
	}

	changedFiles := make([]string, 0)
	for _, rel := range actualFiles {
		actualPath := filepath.Join(actualRoot, rel)
		actualData, err := os.ReadFile(actualPath)
		if err != nil {
			return nil, fmt.Errorf("read actual file %q: %w", rel, err)
		}

		originalPath := filepath.Join(originalRoot, rel)
		originalData, err := os.ReadFile(originalPath)
		if err != nil {
			if os.IsNotExist(err) {
				changedFiles = append(changedFiles, rel)
				continue
			}
			return nil, fmt.Errorf("read original file %q: %w", rel, err)
		}

		if !bytes.Equal(originalData, actualData) {
			changedFiles = append(changedFiles, rel)
		}
	}
	return changedFiles, nil
}
