package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/noninteractive"
)

type CreateOptions struct {
	RepoPath          string
	PackagePath       string
	ModelID           llmmodel.ModelID
	Prompt            string
	OutputDir         string
	IncludeTokenUsage bool
	ProgressOut       io.Writer
	JSONStreamOut     io.Writer
}

type recordedTurn struct {
	Request  map[string]any
	Response map[string]any
}

type recordingDiagnosticHook struct {
	mu    sync.Mutex
	turns []recordedTurn
}

type httpFixtureConfig struct {
	Responses []httpFixtureResponse `json:"responses"`
}

type httpFixtureResponse struct {
	Name     string         `json:"name"`
	Consume  bool           `json:"consume"`
	Request  map[string]any `json:"request"`
	Response map[string]any `json:"response"`
}

func CreateCase(opts CreateOptions) error {
	repoPath, outputDir, err := validateCreateOptions(opts)
	if err != nil {
		return err
	}
	isFixtureRepo, err := isFixtureRepoPath(repoPath)
	if err != nil {
		return fmt.Errorf("check fixture repo path: %w", err)
	}
	reportProgress(opts.ProgressOut, "Preparing integration case output in %s", outputDir)
	reportProgress(opts.ProgressOut, "Copying source repo from %s", repoPath)

	workDir, err := os.MkdirTemp("", "codalotl-integration-create-")
	if err != nil {
		return fmt.Errorf("create temp work dir: %w", err)
	}
	defer os.RemoveAll(workDir)

	if err := copyTree(repoPath, workDir); err != nil {
		return fmt.Errorf("copy repo to temp dir: %w", err)
	}

	hook := &recordingDiagnosticHook{}
	unregister := llmstream.AddDiagnosticHook(hook)
	defer unregister()

	var out bytes.Buffer
	runOut := io.Writer(&out)
	if opts.JSONStreamOut != nil {
		runOut = io.MultiWriter(&out, opts.JSONStreamOut)
	}
	reportProgress(opts.ProgressOut, "Running real agent now. Streaming NDJSON to stdout...")
	err = noninteractive.Exec(opts.Prompt, noninteractive.Options{
		CWD:         workDir,
		PackagePath: opts.PackagePath,
		ModelID:     opts.ModelID,
		OutputJSON:  true,
		AutoYes:     true,
		Out:         runOut,
	})
	if err != nil {
		return fmt.Errorf("run noninteractive exec: %w", err)
	}

	actualEvents, err := parseJSONLines(out.Bytes())
	if err != nil {
		return err
	}
	if len(actualEvents) == 0 {
		return fmt.Errorf("expected at least one JSON event from noninteractive exec")
	}
	if err := assertNoTerminalFailure(actualEvents); err != nil {
		return err
	}
	recordedTurns := hook.snapshot()
	reportProgress(opts.ProgressOut, "Real agent run complete. Captured %d JSON events and %d provider turns.", len(actualEvents), len(recordedTurns))

	caseName := filepath.Base(outputDir)
	cfg, httpCfg, expectedRepoFiles, err := buildGeneratedCase(caseName, repoPath, workDir, actualEvents, recordedTurns, opts)
	if err != nil {
		return err
	}
	reportProgress(opts.ProgressOut, "Writing generated case files...")

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}
	if err := writeConfigJSONFile(filepath.Join(outputDir, "config.json"), cfg); err != nil {
		return err
	}
	if err := writeJSONFile(filepath.Join(outputDir, "http.json"), httpCfg); err != nil {
		return err
	}

	if isFixtureRepo {
		reportProgress(opts.ProgressOut, "Skipping repo snapshot because source repo is the shared fixture repo")
	} else {
		repoOutputDir := filepath.Join(outputDir, "repo")
		if err := copyTree(repoPath, repoOutputDir); err != nil {
			return fmt.Errorf("write repo snapshot: %w", err)
		}
	}
	if err := writeExpectedRepoFiles(filepath.Join(outputDir, "expected_repo"), expectedRepoFiles); err != nil {
		return err
	}

	reportProgress(opts.ProgressOut, "Verifying generated case replay...")
	if err := RunCaseDir(outputDir); err != nil {
		return fmt.Errorf("verify generated case: %w", err)
	}
	reportProgress(opts.ProgressOut, "Created integration case at %s (%d changed files)", outputDir, len(expectedRepoFiles))
	return nil
}

func reportProgress(out io.Writer, format string, args ...any) {
	if out == nil {
		return
	}
	_, _ = fmt.Fprintf(out, format+"\n", args...)
}

func (h *recordingDiagnosticHook) AddTurn(request map[string]any, response map[string]any) {
	if request == nil || response == nil {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	h.turns = append(h.turns, recordedTurn{
		Request:  cloneJSONObject(request),
		Response: cloneJSONObject(response),
	})
}

func (h *recordingDiagnosticHook) snapshot() []recordedTurn {
	if h == nil {
		return nil
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	out := make([]recordedTurn, 0, len(h.turns))
	for _, turn := range h.turns {
		out = append(out, recordedTurn{
			Request:  cloneJSONObject(turn.Request),
			Response: cloneJSONObject(turn.Response),
		})
	}
	return out
}

func validateCreateOptions(opts CreateOptions) (string, string, error) {
	repoPath := strings.TrimSpace(opts.RepoPath)
	if repoPath == "" {
		return "", "", fmt.Errorf("--repo is required")
	}
	repoAbs, err := filepath.Abs(repoPath)
	if err != nil {
		return "", "", fmt.Errorf("resolve repo path: %w", err)
	}

	info, err := os.Stat(repoAbs)
	if err != nil {
		return "", "", fmt.Errorf("stat repo path: %w", err)
	}
	if !info.IsDir() {
		return "", "", fmt.Errorf("--repo must point to a directory")
	}

	if strings.TrimSpace(string(opts.ModelID)) == "" {
		return "", "", fmt.Errorf("--model is required")
	}
	if strings.TrimSpace(opts.Prompt) == "" {
		return "", "", fmt.Errorf("--prompt is required")
	}

	outputDir := strings.TrimSpace(opts.OutputDir)
	if outputDir == "" {
		return "", "", fmt.Errorf("--output is required")
	}
	outputAbs, err := filepath.Abs(outputDir)
	if err != nil {
		return "", "", fmt.Errorf("resolve output dir: %w", err)
	}
	if err := ensureEmptyOrMissingDir(outputAbs); err != nil {
		return "", "", err
	}

	if err := validatePackagePath(repoAbs, opts.PackagePath); err != nil {
		return "", "", err
	}

	return repoAbs, outputAbs, nil
}

func ensureEmptyOrMissingDir(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat output dir: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("--output must be a directory path")
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return fmt.Errorf("read output dir: %w", err)
	}
	if len(entries) != 0 {
		return fmt.Errorf("--output must not already contain files")
	}
	return nil
}

func validatePackagePath(repoPath string, packagePath string) error {
	if packagePath == "" {
		return nil
	}
	if filepath.IsAbs(packagePath) {
		return fmt.Errorf("--package must be relative to --repo")
	}

	pkgAbs := filepath.Join(repoPath, packagePath)
	rel, err := filepath.Rel(repoPath, pkgAbs)
	if err != nil {
		return fmt.Errorf("resolve package path: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("--package must stay within --repo")
	}

	info, err := os.Stat(pkgAbs)
	if err != nil {
		return fmt.Errorf("stat package path: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("--package must point to a directory inside --repo")
	}
	return nil
}

func buildGeneratedCase(caseName string, originalRepoRoot string, actualRepoRoot string, actualEvents []map[string]any, turns []recordedTurn, opts CreateOptions) (testCaseConfig, httpFixtureConfig, map[string]string, error) {
	if len(turns) == 0 {
		return testCaseConfig{}, httpFixtureConfig{}, nil, fmt.Errorf("no diagnostic turns were recorded")
	}

	expected, err := buildExpectedEvents(actualEvents, opts.IncludeTokenUsage, []string{actualRepoRoot})
	if err != nil {
		return testCaseConfig{}, httpFixtureConfig{}, nil, err
	}

	httpCfg, err := buildHTTPFixture(caseName, turns, []string{actualRepoRoot})
	if err != nil {
		return testCaseConfig{}, httpFixtureConfig{}, nil, err
	}

	expectedRepoFiles, err := snapshotExpectedRepoFiles(originalRepoRoot, actualRepoRoot)
	if err != nil {
		return testCaseConfig{}, httpFixtureConfig{}, nil, err
	}

	return testCaseConfig{
		Prompt:      opts.Prompt,
		PackagePath: opts.PackagePath,
		Expected:    expected,
	}, httpCfg, expectedRepoFiles, nil
}

func buildExpectedEvents(actualEvents []map[string]any, includeTokenUsage bool, roots []string) ([]map[string]any, error) {
	expected := make([]map[string]any, 0, len(actualEvents))
	for _, event := range actualEvents {
		cloned := cloneJSONObject(event)
		eventType, _ := cloned["type"].(string)
		switch eventType {
		case "assistant_reasoning":
			continue
		case "start":
			start := map[string]any{
				"type": cloned["type"],
			}
			if packagePath, ok := cloned["package_path"]; ok {
				start["package_path"] = packagePath
			}
			expected = append(expected, start)
			continue
		case "done":
			done := map[string]any{
				"type": cloned["type"],
			}
			if includeTokenUsage {
				if usage, ok := cloned["token_usage"]; ok {
					done["token_usage"] = usage
				}
				if idealUsage, ok := cloned["ideal_token_usage"]; ok {
					done["ideal_token_usage"] = idealUsage
				}
			}
			expected = append(expected, done)
			continue
		}

		if agentValue, ok := cloned["agent"].(map[string]any); ok {
			if depth, ok := agentValue["depth"]; ok {
				cloned["agent"] = map[string]any{"depth": depth}
			} else {
				delete(cloned, "agent")
			}
		}

		if eventType == "tool_complete" {
			result, ok := cloned["result"].(map[string]any)
			if ok {
				if output, ok := result["output"]; ok {
					text, ok := actualMatchText(output)
					if !ok {
						return nil, fmt.Errorf("marshal tool_complete result output")
					}
					result["output"] = buildPartialMatcher(stablePartials(text, roots))
				}
			}
		}

		if eventType == "permission" {
			if prompt, ok := cloned["prompt"].(string); ok && prompt != "" {
				cloned["prompt"] = map[string]any{
					"match": "partial",
					"text":  stableSnippet(prompt, roots),
				}
			}
		}

		expected = append(expected, cloned)
	}
	return expected, nil
}

func buildHTTPFixture(caseName string, turns []recordedTurn, roots []string) (httpFixtureConfig, error) {
	responseIDs := make(map[string]string, len(turns))
	caseSuffix := sanitizeIdentifier(caseName)
	for i, turn := range turns {
		originalID, _ := turn.Response["id"].(string)
		if strings.TrimSpace(originalID) == "" {
			return httpFixtureConfig{}, fmt.Errorf("diagnostic turn %d is missing response.id", i)
		}
		responseIDs[originalID] = fmt.Sprintf("resp_%s_%d", caseSuffix, i+1)
	}

	cfg := httpFixtureConfig{
		Responses: make([]httpFixtureResponse, 0, len(turns)),
	}
	for i, turn := range turns {
		request, err := buildHTTPFixtureRequest(caseSuffix, turn, responseIDs, roots)
		if err != nil {
			return httpFixtureConfig{}, err
		}
		response, err := buildHTTPFixtureResponse(caseSuffix, i, turn.Response, responseIDs[turn.Response["id"].(string)])
		if err != nil {
			return httpFixtureConfig{}, err
		}
		cfg.Responses = append(cfg.Responses, httpFixtureResponse{
			Name:     fmt.Sprintf("turn-%02d", i+1),
			Consume:  true,
			Request:  request,
			Response: response,
		})
	}
	return cfg, nil
}

func buildHTTPFixtureRequest(caseSuffix string, turn recordedTurn, responseIDs map[string]string, roots []string) (map[string]any, error) {
	request := map[string]any{
		"model": "mock-model-" + caseSuffix,
	}

	if prevID, ok := turn.Request["previous_response_id"].(string); ok && prevID != "" {
		mappedPrevID, ok := responseIDs[prevID]
		if !ok {
			return nil, fmt.Errorf("missing normalized previous_response_id for %q", prevID)
		}
		request["previous_response_id"] = mappedPrevID
	}

	inputMatcher, ok := chooseRequestMatcher(turn.Request["input"], roots)
	if !ok {
		return nil, fmt.Errorf("unable to derive stable request input matcher")
	}
	request["input"] = inputMatcher

	if toolName := firstToolName(turn.Response); toolName != "" {
		request["tools"] = map[string]any{
			"match": "partial",
			"text":  fmt.Sprintf("\"name\":\"%s\"", toolName),
		}
	}

	return request, nil
}

func buildHTTPFixtureResponse(caseSuffix string, turnIndex int, response map[string]any, responseID string) (map[string]any, error) {
	fixtureResponse := map[string]any{
		"id":     responseID,
		"object": "response",
	}

	if objectType, ok := response["object"].(string); ok && objectType != "" {
		fixtureResponse["object"] = objectType
	}
	if status, ok := response["status"].(string); ok && status != "" {
		fixtureResponse["status"] = status
	}
	if usage, ok := response["usage"]; ok && usage != nil {
		fixtureResponse["usage"] = cloneJSONValue(usage)
	}
	if errorValue, ok := response["error"]; ok && errorValue != nil {
		fixtureResponse["error"] = cloneJSONValue(errorValue)
	}

	rawOutput, ok := response["output"]
	if !ok {
		fixtureResponse["output"] = []any{}
		return fixtureResponse, nil
	}
	outputItems, ok := rawOutput.([]any)
	if !ok {
		return nil, fmt.Errorf("response.output is not an array")
	}

	normalizedOutput := make([]any, 0, len(outputItems))
	for outputIndex, rawItem := range outputItems {
		item, ok := rawItem.(map[string]any)
		if !ok {
			normalizedOutput = append(normalizedOutput, cloneJSONValue(rawItem))
			continue
		}
		normalizedOutput = append(normalizedOutput, normalizeResponseOutputItem(caseSuffix, turnIndex, outputIndex, item))
	}
	fixtureResponse["output"] = normalizedOutput
	return fixtureResponse, nil
}

func normalizeResponseOutputItem(caseSuffix string, turnIndex int, outputIndex int, item map[string]any) map[string]any {
	itemType, _ := item["type"].(string)
	itemID := fmt.Sprintf("%s_%s_%d_%d", outputItemPrefix(itemType), caseSuffix, turnIndex+1, outputIndex+1)

	switch itemType {
	case "message":
		content, _ := item["content"].([]any)
		normalizedContent := make([]any, 0, len(content))
		for _, rawPart := range content {
			part, ok := rawPart.(map[string]any)
			if !ok {
				continue
			}
			partType, _ := part["type"].(string)
			text, _ := part["text"].(string)
			if partType == "" && text == "" {
				continue
			}
			normalizedContent = append(normalizedContent, map[string]any{
				"type": partType,
				"text": text,
			})
		}
		role, _ := item["role"].(string)
		if role == "" {
			role = "assistant"
		}
		return map[string]any{
			"id":      itemID,
			"type":    "message",
			"role":    role,
			"content": normalizedContent,
		}
	case "function_call":
		normalized := map[string]any{
			"id":        itemID,
			"type":      "function_call",
			"call_id":   stringField(item["call_id"]),
			"name":      stringField(item["name"]),
			"arguments": extractStringValue(item["arguments"]),
		}
		if status := stringField(item["status"]); status != "" {
			normalized["status"] = status
		}
		return normalized
	case "custom_tool_call":
		normalized := map[string]any{
			"id":      itemID,
			"type":    "custom_tool_call",
			"call_id": stringField(item["call_id"]),
			"name":    stringField(item["name"]),
			"input":   extractStringValue(item["input"]),
		}
		if status := stringField(item["status"]); status != "" {
			normalized["status"] = status
		}
		return normalized
	case "reasoning":
		summary, _ := item["summary"].([]any)
		normalizedSummary := make([]any, 0, len(summary))
		for _, rawPart := range summary {
			part, ok := rawPart.(map[string]any)
			if !ok {
				continue
			}
			text, _ := part["text"].(string)
			partType, _ := part["type"].(string)
			if text == "" && partType == "" {
				continue
			}
			entry := map[string]any{
				"text": text,
			}
			if partType != "" {
				entry["type"] = partType
			}
			normalizedSummary = append(normalizedSummary, entry)
		}
		return map[string]any{
			"id":      itemID,
			"type":    "reasoning",
			"summary": normalizedSummary,
		}
	default:
		cloned := cloneJSONObject(item)
		cloned["id"] = itemID
		return cloned
	}
}

func stringField(value any) string {
	text, _ := value.(string)
	return text
}

func extractStringValue(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case map[string]any:
		if text, ok := v["OfString"].(string); ok && text != "" {
			return text
		}

		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if text := extractStringValue(v[key]); text != "" {
				return text
			}
		}
	case []any:
		for _, item := range v {
			if text := extractStringValue(item); text != "" {
				return text
			}
		}
	}
	return ""
}

func outputItemPrefix(itemType string) string {
	switch itemType {
	case "message":
		return "msg"
	case "function_call":
		return "fc"
	case "custom_tool_call":
		return "ct"
	case "reasoning":
		return "rs"
	default:
		return "item"
	}
}

func firstToolName(response map[string]any) string {
	outputItems, ok := response["output"].([]any)
	if !ok {
		return ""
	}
	for _, rawItem := range outputItems {
		item, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}
		itemType, _ := item["type"].(string)
		if itemType != "function_call" && itemType != "custom_tool_call" {
			continue
		}
		name, _ := item["name"].(string)
		return name
	}
	return ""
}

func chooseRequestMatcher(input any, roots []string) (map[string]any, bool) {
	leaves := collectStringLeaves(input)
	var fallback []string
	for i := len(leaves) - 1; i >= 0; i-- {
		partials := stablePartials(leaves[i], roots)
		if len(partials) == 0 {
			continue
		}
		if isLikelyMatcherCandidate(partials) {
			return buildPartialMatcher(partials), true
		}
		if len(fallback) == 0 {
			fallback = partials
		}
	}
	if len(fallback) > 0 {
		return buildPartialMatcher(fallback), true
	}

	text, ok := actualMatchText(input)
	if !ok {
		return nil, false
	}
	partials := stablePartials(text, roots)
	if len(partials) == 0 {
		return nil, false
	}
	return buildPartialMatcher(partials), true
}

func isLikelyMatcherCandidate(partials []string) bool {
	for _, partial := range partials {
		if isLikelySnippetCandidate(partial) {
			return true
		}
	}
	return false
}

func isLikelySnippetCandidate(snippet string) bool {
	if len(snippet) < 8 {
		return false
	}
	return strings.ContainsAny(snippet, " \n\t/<>{}\"`:.")
}

func collectStringLeaves(value any) []string {
	switch v := value.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return []string{v}
	case []any:
		out := make([]string, 0)
		for _, item := range v {
			out = append(out, collectStringLeaves(item)...)
		}
		return out
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		out := make([]string, 0)
		for _, key := range keys {
			out = append(out, collectStringLeaves(v[key])...)
		}
		return out
	default:
		return nil
	}
}

func stableSnippet(text string, roots []string) string {
	partials := stablePartials(text, roots)
	if len(partials) == 0 {
		return ""
	}
	return partials[0]
}

func stablePartials(text string, roots []string) []string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}

	addUnique := func(out []string, candidate string) []string {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			return out
		}
		for _, existing := range out {
			if existing == candidate {
				return out
			}
		}
		return append(out, candidate)
	}

	if !strings.Contains(trimmed, "\n") {
		single := stableSinglePartial(trimmed, roots)
		if single == "" {
			return nil
		}
		return []string{single}
	}

	lines := strings.Split(strings.ReplaceAll(trimmed, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = addUnique(out, stableLinePartial(line, roots))
	}
	return out
}

func stableSinglePartial(text string, roots []string) string {
	for _, root := range roots {
		if candidate := rootRelativeSnippet(text, root); candidate != "" {
			return candidate
		}
	}
	if len(text) > 400 {
		return text[:400]
	}
	return text
}

func stableLinePartial(line string, roots []string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return ""
	}
	for _, root := range roots {
		if candidate := rootRelativeSnippet(trimmed, root); candidate != "" {
			return candidate
		}
	}
	if len(trimmed) > 200 {
		return trimmed[:200]
	}
	return trimmed
}

func buildPartialMatcher(partials []string) map[string]any {
	if len(partials) == 0 {
		return map[string]any{
			"match": "exact",
			"text":  "",
		}
	}
	if len(partials) == 1 {
		return map[string]any{
			"match": "partial",
			"text":  partials[0],
		}
	}
	return map[string]any{
		"match": "partial",
		"texts": partials,
	}
}

func rootRelativeSnippet(text string, root string) string {
	if root == "" {
		return ""
	}

	idx := strings.Index(text, root)
	for idx >= 0 {
		tail := text[idx+len(root):]
		tail = strings.TrimLeft(tail, `/\`)
		if candidate := leadingPathToken(tail); candidate != "" {
			return candidate
		}
		next := strings.Index(text[idx+len(root):], root)
		if next < 0 {
			break
		}
		idx += len(root) + next
	}
	return ""
}

func leadingPathToken(text string) string {
	var b strings.Builder
	for _, r := range text {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' || r == '/' {
			b.WriteRune(r)
			continue
		}
		break
	}
	token := strings.Trim(b.String(), "/")
	if strings.Count(token, "/") == 0 && !strings.Contains(token, ".") {
		return ""
	}
	return token
}

func snapshotExpectedRepoFiles(originalRoot string, actualRoot string) (map[string]string, error) {
	files, err := changedOrCreatedFiles(originalRoot, actualRoot)
	if err != nil {
		return nil, err
	}

	out := make(map[string]string, len(files))
	for _, rel := range files {
		data, err := os.ReadFile(filepath.Join(actualRoot, rel))
		if err != nil {
			return nil, fmt.Errorf("read changed file %q: %w", rel, err)
		}
		out[rel] = string(data)
	}
	return out, nil
}

func writeExpectedRepoFiles(root string, files map[string]string) error {
	if len(files) == 0 {
		return nil
	}

	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	for _, rel := range paths {
		targetPath := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return fmt.Errorf("create expected repo dir: %w", err)
		}
		if err := os.WriteFile(targetPath, []byte(files[rel]), 0o644); err != nil {
			return fmt.Errorf("write expected repo file %q: %w", rel, err)
		}
	}
	return nil
}

func writeJSONFile(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create json parent dir: %w", err)
	}

	data, err := marshalPrettyJSON(value)
	if err != nil {
		return fmt.Errorf("marshal %q: %w", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %q: %w", path, err)
	}
	return nil
}

func writeConfigJSONFile(path string, cfg testCaseConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create json parent dir: %w", err)
	}

	data, err := marshalConfigJSON(cfg)
	if err != nil {
		return fmt.Errorf("marshal %q: %w", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %q: %w", path, err)
	}
	return nil
}

func marshalPrettyJSON(value any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(value); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func marshalCompactJSON(value any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

func marshalConfigJSON(cfg testCaseConfig) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("{\n")

	prompt, err := marshalCompactJSON(cfg.Prompt)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(&buf, "  \"prompt\": %s", prompt)

	if cfg.PackagePath != "" {
		packagePath, err := marshalCompactJSON(cfg.PackagePath)
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&buf, ",\n  \"package_path\": %s", packagePath)
	}

	buf.WriteString(",\n  \"expected\": [\n")
	for i, event := range cfg.Expected {
		eventJSON, err := marshalCompactJSON(event)
		if err != nil {
			return nil, err
		}
		buf.WriteString("    ")
		buf.Write(eventJSON)
		if i < len(cfg.Expected)-1 {
			buf.WriteString(",")
		}
		buf.WriteString("\n")
	}
	buf.WriteString("  ]\n}\n")
	return buf.Bytes(), nil
}

func cloneJSONObject(value map[string]any) map[string]any {
	cloned, _ := cloneJSONValue(value).(map[string]any)
	return cloned
}

func cloneJSONValue(value any) any {
	data, err := json.Marshal(value)
	if err != nil {
		return value
	}

	var cloned any
	if err := json.Unmarshal(data, &cloned); err != nil {
		return value
	}
	return cloned
}
