package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/build"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/codalotl/codalotl/internal/lints"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/noninteractive"
	"github.com/codalotl/codalotl/internal/q/cmdrunner"
)

type CreateOptions struct {
	RepoPath          string
	PackagePath       string
	ModelID           llmmodel.ModelID
	Prompt            string
	OutputDir         string
	ReflowWidth       int
	Lints             lints.Lints
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

const (
	httpFixtureRepoRootPlaceholder   = "__REPO_ROOT__"
	httpFixtureGoRootSrcPlaceholder  = "__GOROOT_SRC__"
	httpFixtureGoModCachePlaceholder = "__GOMODCACHE__"
)

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
	lintSteps, err := lints.ResolveSteps(&opts.Lints, opts.ReflowWidth)
	if err != nil {
		return fmt.Errorf("resolve lint steps: %w", err)
	}
	reportProgress(opts.ProgressOut, "Running real agent now. Streaming NDJSON to stdout...")
	err = noninteractive.Exec(opts.Prompt, noninteractive.Options{
		CWD:         workDir,
		PackagePath: opts.PackagePath,
		ModelID:     opts.ModelID,
		LintSteps:   lintSteps,
		ReflowWidth: opts.ReflowWidth,
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
		Prompt:      normalizeConfigPromptText(opts.Prompt, []string{actualRepoRoot}),
		PackagePath: opts.PackagePath,
		ReflowWidth: opts.ReflowWidth,
		Lints:       opts.Lints,
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
		expected = append(expected, normalizeJSONObjectAbsolutePaths(cloned, roots))
	}
	return expected, nil
}

func buildHTTPFixture(caseName string, turns []recordedTurn, roots []string) (httpFixtureConfig, error) {
	caseSuffix := sanitizeIdentifier(caseName)
	cfg := httpFixtureConfig{
		Responses: make([]httpFixtureResponse, 0, len(turns)),
	}
	for i, turn := range turns {
		request, err := buildHTTPFixtureRequest(caseSuffix, i == 0, turn, roots)
		if err != nil {
			return httpFixtureConfig{}, err
		}
		response, err := buildHTTPFixtureResponse(turn.Response, roots)
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

func buildHTTPFixtureRequest(caseSuffix string, firstTurn bool, turn recordedTurn, roots []string) (map[string]any, error) {
	request, err := buildPrunedHTTPFixtureRequest(turn.Request, roots, firstTurn)
	if err != nil {
		return nil, err
	}
	request["model"] = "mock-model-" + caseSuffix
	return request, nil
}

func buildReplayDebugHTTPFixtureRequest(request map[string]any, roots []string) (map[string]any, error) {
	return buildPrunedHTTPFixtureRequest(request, roots, isFirstHTTPFixtureTurnRequest(request))
}

func buildPrunedHTTPFixtureRequest(request map[string]any, roots []string, firstTurn bool) (map[string]any, error) {
	pruned := normalizeHTTPJSONObjectAbsolutePaths(cloneJSONObject(request), roots)
	if pruned == nil {
		return nil, fmt.Errorf("request must be a JSON object")
	}
	pruneHTTPFixtureRequest(pruned, firstTurn)
	return pruned, nil
}

func isFirstHTTPFixtureTurnRequest(request map[string]any) bool {
	previousResponseID, ok := request["previous_response_id"]
	if !ok {
		return true
	}

	responseID, ok := previousResponseID.(string)
	return !ok || responseID == ""
}

func pruneHTTPFixtureRequest(request map[string]any, firstTurn bool) {
	if firstTurn {
		pruneFirstTurnHTTPFixtureInput(request)
	}
	pruneHTTPFixtureRequestFields(request)
}

func pruneFirstTurnHTTPFixtureInput(request map[string]any) {
	input, ok := request["input"].([]any)
	if !ok || len(input) == 0 {
		return
	}
	pruned := append([]any(nil), input...)
	limit := min(2, len(pruned))
	for i := 0; i < limit; i++ {
		pruned[i] = omitTextKeys(pruned[i])
	}
	request["input"] = pruned
}

func pruneHTTPFixtureRequestFields(request map[string]any) {
	delete(request, "tools")
	delete(request, "prompt_cache_key")
	delete(request, "reasoning")
	delete(request, "parallel_tool_calls")
	delete(request, "store")
	delete(request, "stream")
	delete(request, "context_management")
}

func omitTextKeys(value any) any {
	switch v := value.(type) {
	case []any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, omitTextKeys(item))
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			if key == "text" {
				continue
			}
			out[key] = omitTextKeys(item)
		}
		return out
	default:
		return value
	}
}

func buildHTTPFixtureResponse(response map[string]any, roots []string) (map[string]any, error) {
	responseID, _ := response["id"].(string)
	if strings.TrimSpace(responseID) == "" {
		return nil, fmt.Errorf("response.id is required")
	}
	fixtureResponse := normalizeHTTPResponseForFixture(cloneJSONObject(response), roots)
	return fixtureResponse, nil
}

func normalizeHTTPResponseForFixture(response map[string]any, roots []string) map[string]any {
	normalized := normalizeHTTPJSONObjectAbsolutePaths(response, roots)

	outputItems, ok := normalized["output"].([]any)
	if !ok {
		return normalized
	}

	for i, rawItem := range outputItems {
		item, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}
		outputItems[i] = normalizeHTTPResponseOutputItem(item)
	}
	normalized["output"] = outputItems
	return normalized
}

func normalizeHTTPResponseOutputItem(item map[string]any) map[string]any {
	normalized := cloneJSONObject(item)
	if normalized == nil {
		return nil
	}
	if _, ok := normalized["arguments"]; ok {
		normalized["arguments"] = extractStringValue(normalized["arguments"])
	}
	if _, ok := normalized["input"]; ok {
		normalized["input"] = extractStringValue(normalized["input"])
	}
	return normalized
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

func normalizeJSONObjectAbsolutePaths(value map[string]any, roots []string) map[string]any {
	return cloneJSONObjectFromValue(normalizeJSONAbsolutePaths(value, roots))
}

func normalizeHTTPJSONObjectAbsolutePaths(value map[string]any, roots []string) map[string]any {
	return cloneJSONObjectFromValue(normalizeHTTPJSONAbsolutePaths(value, roots))
}

func cloneJSONObjectFromValue(value any) map[string]any {
	cloned, _ := value.(map[string]any)
	return cloned
}

func normalizeJSONAbsolutePaths(value any, roots []string) any {
	switch v := value.(type) {
	case string:
		return normalizeAbsolutePathText(v, roots)
	case []any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, normalizeJSONAbsolutePaths(item, roots))
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[key] = normalizeJSONAbsolutePaths(item, roots)
		}
		return out
	default:
		return value
	}
}

func normalizeHTTPJSONAbsolutePaths(value any, roots []string) any {
	switch v := value.(type) {
	case string:
		return normalizeHTTPAbsolutePathText(v, roots)
	case []any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, normalizeHTTPJSONAbsolutePaths(item, roots))
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[key] = normalizeHTTPJSONAbsolutePaths(item, roots)
		}
		return out
	default:
		return value
	}
}

func denormalizeHTTPJSONAbsolutePaths(value any, roots []string) any {
	switch v := value.(type) {
	case string:
		return denormalizeHTTPAbsolutePathText(v, roots)
	case []any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, denormalizeHTTPJSONAbsolutePaths(item, roots))
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[key] = denormalizeHTTPJSONAbsolutePaths(item, roots)
		}
		return out
	default:
		return value
	}
}

func normalizeConfigPromptText(text string, roots []string) string {
	return normalizeHTTPAbsolutePathText(text, roots)
}

func denormalizeConfigPromptText(text string, roots []string) string {
	return denormalizeHTTPAbsolutePathText(text, roots)
}

type pathReplacement struct {
	prefix      string
	replacement string
}

func normalizeAbsolutePathText(text string, roots []string) string {
	replacements := absolutePathReplacements(roots)
	if len(replacements) == 0 {
		return text
	}

	normalized := text
	for _, replacement := range replacements {
		normalized = replaceAbsolutePathPrefix(normalized, replacement.prefix, replacement.replacement)
	}
	return normalized
}

func normalizeHTTPAbsolutePathText(text string, roots []string) string {
	return replacePathPrefixes(text, httpFixturePathReplacements(roots))
}

func denormalizeHTTPAbsolutePathText(text string, roots []string) string {
	return replacePathPrefixes(text, httpFixturePlaceholderReplacements(roots))
}

func replacePathPrefixes(text string, replacements []pathReplacement) string {
	if len(replacements) == 0 {
		return text
	}

	normalized := text
	for _, replacement := range replacements {
		normalized = replaceAbsolutePathPrefix(normalized, replacement.prefix, replacement.replacement)
	}
	return normalized
}

func absolutePathReplacements(roots []string) []pathReplacement {
	replacements := newPathReplacementBuilder(len(roots) + 2)
	for _, root := range roots {
		replacements.add(root, "")
	}

	if goroot := build.Default.GOROOT; goroot != "" {
		replacements.add(filepath.Join(goroot, "src"), "stdlib")
	}
	if modcache := goModCachePath(); modcache != "" {
		replacements.add(modcache, "modcache")
	}
	return replacements.build()
}

func httpFixturePathReplacements(roots []string) []pathReplacement {
	replacements := newPathReplacementBuilder(len(roots) + 2)
	for _, root := range roots {
		replacements.add(root, httpFixtureRepoRootPlaceholder)
	}

	if goroot := build.Default.GOROOT; goroot != "" {
		replacements.add(filepath.Join(goroot, "src"), httpFixtureGoRootSrcPlaceholder)
	}
	if modcache := goModCachePath(); modcache != "" {
		replacements.add(modcache, httpFixtureGoModCachePlaceholder)
	}
	return replacements.build()
}

func httpFixturePlaceholderReplacements(roots []string) []pathReplacement {
	replacements := newPathReplacementBuilder(len(roots) + 2)
	for _, root := range roots {
		replacements.add(httpFixtureRepoRootPlaceholder, filepath.Clean(root))
	}

	if goroot := build.Default.GOROOT; goroot != "" {
		replacements.add(httpFixtureGoRootSrcPlaceholder, filepath.Join(goroot, "src"))
	}
	if modcache := goModCachePath(); modcache != "" {
		replacements.add(httpFixtureGoModCachePlaceholder, modcache)
	}
	return replacements.build()
}

type pathReplacementBuilder struct {
	replacements []pathReplacement
	seen         map[string]struct{}
}

func newPathReplacementBuilder(capacity int) pathReplacementBuilder {
	return pathReplacementBuilder{
		replacements: make([]pathReplacement, 0, capacity),
		seen:         make(map[string]struct{}, capacity),
	}
}

func (b *pathReplacementBuilder) add(prefix string, replacement string) {
	if prefix == "" {
		return
	}

	cleanPrefix := filepath.Clean(prefix)
	if _, ok := b.seen[cleanPrefix]; ok {
		return
	}
	b.seen[cleanPrefix] = struct{}{}
	b.replacements = append(b.replacements, pathReplacement{
		prefix:      cleanPrefix,
		replacement: replacement,
	})
}

func (b pathReplacementBuilder) build() []pathReplacement {
	sort.Slice(b.replacements, func(i int, j int) bool {
		return len(b.replacements[i].prefix) > len(b.replacements[j].prefix)
	})
	return b.replacements
}

func goModCachePath() string {
	if env := os.Getenv("GOMODCACHE"); env != "" {
		return env
	}
	if gopath := os.Getenv("GOPATH"); gopath != "" {
		return filepath.Join(gopath, "pkg", "mod")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, "go", "pkg", "mod")
	}
	return ""
}

func replaceAbsolutePathPrefix(text string, prefix string, replacement string) string {
	if text == "" || prefix == "" {
		return text
	}

	var out strings.Builder
	searchFrom := 0
	lastWritten := 0
	for searchFrom < len(text) {
		idx := strings.Index(text[searchFrom:], prefix)
		if idx < 0 {
			break
		}
		idx += searchFrom
		end := idx + len(prefix)
		if !pathPrefixHasBoundary(text, idx, end) {
			searchFrom = end
			continue
		}

		out.WriteString(text[lastWritten:idx])
		switch {
		case end == len(text):
			if replacement == "" {
				out.WriteString(".")
			} else {
				out.WriteString(replacement)
			}
			lastWritten = end
		case text[end] == '/' || text[end] == '\\':
			if replacement != "" {
				out.WriteString(replacement)
				out.WriteByte('/')
			}
			lastWritten = end + 1
		default:
			if replacement == "" {
				out.WriteString(".")
			} else {
				out.WriteString(replacement)
			}
			lastWritten = end
		}
		searchFrom = lastWritten
	}
	if lastWritten == 0 {
		return text
	}
	out.WriteString(text[lastWritten:])
	return out.String()
}

func pathPrefixHasBoundary(text string, start int, end int) bool {
	if start > 0 && isPathTokenByte(text[start-1]) {
		return false
	}
	if end < len(text) && isPathTokenByte(text[end]) && text[end] != '/' && text[end] != '\\' {
		return false
	}
	return true
}

func isPathTokenByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z':
		return true
	case b >= 'A' && b <= 'Z':
		return true
	case b >= '0' && b <= '9':
		return true
	}
	switch b {
	case '.', '_', '-', '/', '\\', '~':
		return true
	default:
		return false
	}
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
	if cfg.ReflowWidth > 0 {
		reflowWidth, err := marshalCompactJSON(cfg.ReflowWidth)
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&buf, ",\n  \"reflowwidth\": %s", reflowWidth)
	}
	if hasLintsConfig(cfg.Lints) {
		lintsJSON, err := marshalPrettyJSON(normalizeLintsConfigJSON(cfg.Lints))
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&buf, ",\n  \"lints\": %s", strings.ReplaceAll(string(bytes.TrimSuffix(lintsJSON, []byte("\n"))), "\n", "\n  "))
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

func hasLintsConfig(cfg lints.Lints) bool {
	return cfg.Mode != "" || len(cfg.Disable) > 0 || len(cfg.Steps) > 0
}

func normalizeLintsConfigJSON(cfg lints.Lints) map[string]any {
	out := make(map[string]any)
	if cfg.Mode != "" {
		out["mode"] = cfg.Mode
	}
	if len(cfg.Disable) > 0 {
		out["disable"] = append([]string(nil), cfg.Disable...)
	}
	if len(cfg.Steps) > 0 {
		steps := make([]map[string]any, 0, len(cfg.Steps))
		for _, step := range cfg.Steps {
			steps = append(steps, normalizeLintStepJSON(step))
		}
		out["steps"] = steps
	}
	return out
}

func normalizeLintStepJSON(step lints.Step) map[string]any {
	out := make(map[string]any)
	if step.ID != "" {
		out["id"] = step.ID
	}
	if len(step.Situations) > 0 {
		out["situations"] = append([]lints.Situation(nil), step.Situations...)
	}
	if step.Active != nil {
		out["active"] = normalizeLintCommandJSON(step.Active)
	}
	if step.Check != nil {
		out["check"] = normalizeLintCommandJSON(step.Check)
	}
	if step.Fix != nil {
		out["fix"] = normalizeLintCommandJSON(step.Fix)
	}
	return out
}

func normalizeLintCommandJSON(cmd *cmdrunner.Command) map[string]any {
	out := map[string]any{
		"command": cmd.Command,
	}
	if len(cmd.Args) > 0 {
		out["args"] = append([]string(nil), cmd.Args...)
	}
	if cmd.CWD != "" {
		out["cwd"] = cmd.CWD
	}
	if len(cmd.Env) > 0 {
		out["env"] = append([]string(nil), cmd.Env...)
	}
	if cmd.OutcomeFailIfAnyOutput {
		out["outcomefailifanyoutput"] = true
	}
	if cmd.MessageIfNoOutput != "" {
		out["messageifnooutput"] = cmd.MessageIfNoOutput
	}
	if cmd.ShowCWD {
		out["showcwd"] = true
	}
	if len(cmd.Attrs) > 0 {
		out["attrs"] = append([]string(nil), cmd.Attrs...)
	}
	return out
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
