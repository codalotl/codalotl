package mockopenai

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"unicode/utf8"
)

const (
	pathResponses   = "/responses"
	pathV1Responses = "/v1/responses"
	matchExact      = "exact"
	matchPartial    = "partial"
	chunkRunes      = 24
)

// The rawConfig type is the top-level JSON fixture format before validation and compilation.
type rawConfig struct {
	Responses []rawResponse `json:"responses"` // Responses contains the mock responses checked in fixture order.
}

// The rawResponse type is one mock response entry before validation and compilation.
type rawResponse struct {
	Name     string                     `json:"name"`     // Name is an optional label used in diagnostics.
	Consume  bool                       `json:"consume"`  // Consume makes the response unavailable after its first match.
	Request  map[string]json.RawMessage `json:"request"`  // Request maps request body fields to matcher definitions.
	Headers  []rawHeader                `json:"headers"`  // Headers lists request header matchers required for this response.
	Response json.RawMessage            `json:"response"` // Response is the JSON payload streamed when this entry matches.
}

// The rawHeader type is one configured request header matcher before compilation.
type rawHeader struct {
	Name  string          `json:"name"`  // Name is the HTTP header name to match.
	Value json.RawMessage `json:"value"` // Value is the matcher definition applied to the header's values.
}

// The compiledResponse type is one mock response entry prepared for runtime matching.
type compiledResponse struct {
	name            string                  // Name is the optional diagnostic label from the fixture.
	consume         bool                    // Consume reports whether this response may be matched only once.
	requestMatchers map[string]valueMatcher // RequestMatchers contains compiled matchers for request body fields.
	headerMatchers  []headerMatcher         // HeaderMatchers contains compiled request header matchers.
	response        any                     // Response is the decoded JSON payload streamed when this entry matches.
	consumed        bool                    // Consumed reports whether a consume-on-use response has already matched.
}

// The headerMatcher type is a compiled matcher for one HTTP request header.
type headerMatcher struct {
	name  string       // Name is the HTTP header name to match.
	value valueMatcher // Value matches at least one value of the named header.
}

// The valueMatcher type matches a decoded JSON value or header value against a compiled matcher definition.
type valueMatcher struct {
	matchType  string                  // MatchType is the text matching mode, such as exact or partial.
	text       string                  // Text is the single text fragment used for exact or partial text matching.
	texts      []string                // Texts are ordered text fragments required for partial matching without overlap.
	hasLiteral bool                    // HasLiteral reports whether literal contains an exact JSON value matcher.
	literal    string                  // Literal is the canonical JSON representation required for exact literal matching.
	object     map[string]valueMatcher // Object contains recursive field matchers for JSON object subset matching.
	array      []valueMatcher          // Array contains positional matchers for JSON arrays with the same length.
}

// The handler type serves mock OpenAI Responses API requests and tracks matching state.
type handler struct {
	mu                   sync.Mutex         // Mu protects responses and lastUnmatchedRequest.
	responses            []compiledResponse // Responses contains the configured mock responses in matching order.
	lastUnmatchedRequest map[string]any     // LastUnmatchedRequest is a cloned decoded body from the most recent unmatched request.
}

// DebugState describes recent matching state for a mock handler.
type DebugState struct {
	LastUnmatchedRequest        map[string]any // LastUnmatchedRequest is the decoded body from the most recent unmatched request, or nil if none is recorded.
	NextUnconsumedConsumedIndex int            // NextUnconsumedConsumedIndex is the next unmatched consume-on-use response index, or -1 if none remain.
}

// NewHandlerFromFile creates a mock OpenAI Responses API handler from a JSON or JSON-with-comments file.
//
// The file may include line comments, block comments, and trailing commas. The returned handler accepts POST requests to /responses and /v1/responses.
func NewHandlerFromFile(path string) (http.Handler, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read mock OpenAI responses file: %w", err)
	}

	return NewHandler(data)
}

// NewHandler creates a mock OpenAI Responses API handler from JSON or JSON-with-comments bytes.
//
// Configured responses are checked in order, and the first matching response is streamed back as SSE. Matching can include request body fields, request headers,
// and consume-on-use behavior; see the package documentation for the configuration format.
func NewHandler(data []byte) (http.Handler, error) {
	responses, err := parseConfig(data)
	if err != nil {
		return nil, err
	}

	return &handler{responses: responses}, nil
}

// AssertAllConsumed reports whether every configured response with `consume: true` was matched.
//
// It returns an error listing any configured responses that were never used. If h was not created by NewHandler or NewHandlerFromFile, AssertAllConsumed returns
// an error.
func AssertAllConsumed(h http.Handler) error {
	mockHandler, ok := h.(*handler)
	if !ok {
		return fmt.Errorf("handler is not a mockopenai handler")
	}
	return mockHandler.assertAllConsumed()
}

// DebugInfo returns the last unmatched request and the next unconsumed response index.
func DebugInfo(h http.Handler) (DebugState, error) {
	mockHandler, ok := h.(*handler)
	if !ok {
		return DebugState{}, fmt.Errorf("handler is not a mockopenai handler")
	}
	return mockHandler.debugState(), nil
}

// The assertAllConsumed method reports an error for each consume-on-use response that has not been matched.
func (h *handler) assertAllConsumed() error {
	if h == nil {
		return nil
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	var unused []string
	for i, response := range h.responses {
		if !response.consume || response.consumed {
			continue
		}

		name := strings.TrimSpace(response.name)
		if name == "" {
			name = fmt.Sprintf("response[%d]", i)
		}
		unused = append(unused, name)
	}

	if len(unused) == 0 {
		return nil
	}
	return fmt.Errorf("unused consumed mock responses: %s", strings.Join(unused, ", "))
}

func parseConfig(data []byte) ([]compiledResponse, error) {
	cleaned := stripTrailingCommas(stripComments(data))

	var cfg rawConfig
	if err := json.Unmarshal(cleaned, &cfg); err != nil {
		return nil, fmt.Errorf("parse mock OpenAI responses config: %w", err)
	}

	compiled := make([]compiledResponse, 0, len(cfg.Responses))
	for i, response := range cfg.Responses {
		compiledResponse, err := compileResponse(response)
		if err != nil {
			return nil, fmt.Errorf("compile response %d: %w", i, err)
		}
		compiled = append(compiled, compiledResponse)
	}

	return compiled, nil
}

// The compileResponse function validates one raw fixture response and converts its matchers and response payload into runtime form.
func compileResponse(raw rawResponse) (compiledResponse, error) {
	compiled := compiledResponse{
		name:            raw.Name,
		consume:         raw.Consume,
		requestMatchers: make(map[string]valueMatcher, len(raw.Request)),
		headerMatchers:  make([]headerMatcher, 0, len(raw.Headers)),
	}

	for field, matcherData := range raw.Request {
		matcher, err := parseValueMatcher(matcherData)
		if err != nil {
			return compiledResponse{}, fmt.Errorf("request field %q: %w", field, err)
		}
		compiled.requestMatchers[field] = matcher
	}

	for _, header := range raw.Headers {
		if header.Name == "" {
			return compiledResponse{}, fmt.Errorf("header name must not be empty")
		}

		matcher, err := parseValueMatcher(header.Value)
		if err != nil {
			return compiledResponse{}, fmt.Errorf("header %q: %w", header.Name, err)
		}

		compiled.headerMatchers = append(compiled.headerMatchers, headerMatcher{
			name:  header.Name,
			value: matcher,
		})
	}

	if len(raw.Response) == 0 {
		return compiledResponse{}, fmt.Errorf("response must not be empty")
	}
	if err := json.Unmarshal(raw.Response, &compiled.response); err != nil {
		return compiledResponse{}, fmt.Errorf("parse response JSON: %w", err)
	}

	return compiled, nil
}

func parseValueMatcher(data json.RawMessage) (valueMatcher, error) {
	matcher, _, err := parseValueMatcherInternal(data)
	return matcher, err
}

// The parseValueMatcherInternal function parses a raw JSON matcher into its recursive runtime representation.
func parseValueMatcherInternal(data json.RawMessage) (valueMatcher, bool, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err == nil {
		if matcher, ok, err := parseDirectTextMatcher(object); ok || err != nil {
			return matcher, true, err
		}

		fields := make(map[string]valueMatcher, len(object))
		for key, rawValue := range object {
			matcher, _, err := parseValueMatcherInternal(rawValue)
			if err != nil {
				return valueMatcher{}, false, fmt.Errorf("field %q: %w", key, err)
			}
			fields[key] = matcher
		}

		return valueMatcher{object: fields}, true, nil
	}

	var array []json.RawMessage
	if err := json.Unmarshal(data, &array); err == nil {
		items := make([]valueMatcher, 0, len(array))
		for index, rawValue := range array {
			matcher, _, err := parseValueMatcherInternal(rawValue)
			if err != nil {
				return valueMatcher{}, false, fmt.Errorf("index %d: %w", index, err)
			}
			items = append(items, matcher)
		}

		return valueMatcher{array: items}, true, nil
	}

	matcher, err := parseLiteralMatcher(data)
	return matcher, false, err
}

// The parseDirectTextMatcher function parses a direct text matcher object and reports whether the object has that matcher shape.
func parseDirectTextMatcher(object map[string]json.RawMessage) (valueMatcher, bool, error) {
	if len(object) == 0 {
		return valueMatcher{}, false, nil
	}

	if !hasOnlyKeys(object, "match", "text") && !hasOnlyKeys(object, "match", "texts") && !hasOnlyKeys(object, "text") && !hasOnlyKeys(object, "texts") {
		return valueMatcher{}, false, nil
	}

	if rawText, ok := object["text"]; ok {
		if _, hasTexts := object["texts"]; hasTexts {
			return valueMatcher{}, true, fmt.Errorf("text matcher cannot include both %q and %q", "text", "texts")
		}

		var text string
		if err := json.Unmarshal(rawText, &text); err != nil {
			return valueMatcher{}, true, fmt.Errorf("parse text matcher text: %w", err)
		}

		matchType := matchExact
		if rawMatchType, ok := object["match"]; ok {
			if err := json.Unmarshal(rawMatchType, &matchType); err != nil {
				return valueMatcher{}, true, fmt.Errorf("parse text matcher match type: %w", err)
			}
		}

		switch matchType {
		case matchExact, matchPartial:
			return valueMatcher{
				matchType: matchType,
				text:      text,
			}, true, nil
		default:
			return valueMatcher{}, true, fmt.Errorf("unsupported match type %q", matchType)
		}
	}

	rawTexts, ok := object["texts"]
	if !ok {
		return valueMatcher{}, false, nil
	}

	var texts []string
	if err := json.Unmarshal(rawTexts, &texts); err != nil {
		return valueMatcher{}, true, fmt.Errorf("parse text matcher texts: %w", err)
	}
	if len(texts) == 0 {
		return valueMatcher{}, true, fmt.Errorf("parse text matcher texts: must not be empty")
	}

	matchType := matchPartial
	if rawMatchType, ok := object["match"]; ok {
		if err := json.Unmarshal(rawMatchType, &matchType); err != nil {
			return valueMatcher{}, true, fmt.Errorf("parse text matcher match type: %w", err)
		}
	}
	if matchType != matchPartial {
		return valueMatcher{}, true, fmt.Errorf("unsupported match type %q", matchType)
	}

	return valueMatcher{
		matchType: matchType,
		texts:     texts,
	}, true, nil
}

func hasOnlyKeys(object map[string]json.RawMessage, keys ...string) bool {
	if len(object) != len(keys) {
		return false
	}
	for _, key := range keys {
		if _, ok := object[key]; !ok {
			return false
		}
	}
	return true
}

func parseLiteralMatcher(data json.RawMessage) (valueMatcher, error) {
	var literal any
	if err := json.Unmarshal(data, &literal); err != nil {
		return valueMatcher{}, fmt.Errorf("parse matcher: %w", err)
	}

	canonical, err := canonicalJSON(literal)
	if err != nil {
		return valueMatcher{}, fmt.Errorf("canonicalize matcher: %w", err)
	}

	return valueMatcher{
		matchType:  matchExact,
		hasLiteral: true,
		literal:    canonical,
	}, nil
}

// ServeHTTP handles mock Responses API HTTP requests and streams the matching response as SSE.
func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != pathResponses && r.URL.Path != pathV1Responses {
		http.NotFound(w, r)
		return
	}

	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read request body", http.StatusBadRequest)
		return
	}

	var request map[string]any
	if err := json.Unmarshal(body, &request); err != nil {
		http.Error(w, "invalid JSON request body", http.StatusBadRequest)
		return
	}

	response, ok := h.matchResponse(request, r.Header)
	if !ok {
		http.Error(w, "no matching mock OpenAI response", http.StatusNotFound)
		return
	}

	if err := writeSSE(w, response); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// The matchResponse method returns the first configured response that matches the request body and headers.
func (h *handler) matchResponse(request map[string]any, headers http.Header) (any, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for i := range h.responses {
		response := &h.responses[i]
		if response.consume && response.consumed {
			continue
		}
		if !matchesRequest(response.requestMatchers, request) {
			continue
		}
		if !matchesHeaders(response.headerMatchers, headers) {
			continue
		}

		if response.consume {
			response.consumed = true
		}
		h.lastUnmatchedRequest = nil

		return response.response, true
	}

	h.lastUnmatchedRequest = cloneJSONObject(request)
	return nil, false
}

// The debugState method returns a snapshot of the handler's recent unmatched request and consume-on-use progress.
func (h *handler) debugState() DebugState {
	if h == nil {
		return DebugState{NextUnconsumedConsumedIndex: -1}
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	state := DebugState{
		NextUnconsumedConsumedIndex: -1,
	}
	if h.lastUnmatchedRequest != nil {
		state.LastUnmatchedRequest = cloneJSONObject(h.lastUnmatchedRequest)
	}
	for i, response := range h.responses {
		if response.consume && !response.consumed {
			state.NextUnconsumedConsumedIndex = i
			break
		}
	}
	return state
}

func matchesRequest(matchers map[string]valueMatcher, request map[string]any) bool {
	for field, matcher := range matchers {
		actual, ok := request[field]
		if !ok {
			return false
		}
		if !matcher.matches(actual) {
			return false
		}
	}

	return true
}

// The matchesHeaders function reports whether all configured header matchers are satisfied by the request headers.
func matchesHeaders(matchers []headerMatcher, headers http.Header) bool {
	for _, matcher := range matchers {
		values := headers.Values(matcher.name)
		if len(values) == 0 {
			return false
		}

		matched := false
		for _, value := range values {
			if matcher.value.matches(value) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	return true
}

// The matches method reports whether actual satisfies the matcher.
func (m valueMatcher) matches(actual any) bool {
	if m.object != nil {
		actualObject, ok := actual.(map[string]any)
		if !ok {
			return false
		}

		for key, matcher := range m.object {
			actualValue, ok := actualObject[key]
			if !ok {
				return false
			}
			if !matcher.matches(actualValue) {
				return false
			}
		}

		return true
	}

	if m.array != nil {
		actualArray, ok := actual.([]any)
		if !ok {
			return false
		}
		if len(actualArray) != len(m.array) {
			return false
		}
		for index, matcher := range m.array {
			if !matcher.matches(actualArray[index]) {
				return false
			}
		}
		return true
	}

	if m.hasLiteral {
		canonical, err := canonicalJSON(actual)
		if err != nil {
			return false
		}
		return canonical == m.literal
	}

	actualText, ok := actualMatchText(actual)
	if !ok {
		return false
	}

	if len(m.texts) > 0 {
		if m.matchType != matchPartial {
			return false
		}
		return containsTextsInOrder(actualText, m.texts) || structuredValueContainsTextsInOrder(actual, m.texts)
	}

	switch m.matchType {
	case matchExact:
		return actualText == m.text
	case matchPartial:
		return strings.Contains(actualText, m.text) || structuredValueContainsText(actual, m.text)
	default:
		return false
	}
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

func structuredValueContainsTextsInOrder(actual any, needles []string) bool {
	switch value := actual.(type) {
	case string:
		return containsTextsInOrder(value, needles)
	case []any:
		for _, item := range value {
			if structuredValueContainsTextsInOrder(item, needles) {
				return true
			}
		}
	case map[string]any:
		for _, item := range value {
			if structuredValueContainsTextsInOrder(item, needles) {
				return true
			}
		}
	}
	return false
}

func containsTextsInOrder(actualText string, texts []string) bool {
	searchStart := 0

	for _, text := range texts {
		matchIndex := strings.Index(actualText[searchStart:], text)
		if matchIndex < 0 {
			return false
		}
		searchStart += matchIndex + len(text)
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
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return nil, err
	}
	return []byte(strings.TrimSuffix(buf.String(), "\n")), nil
}

func canonicalJSON(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}

	return string(encoded), nil
}

// The writeSSE function writes a matched response as a server-sent event stream.
func writeSSE(w http.ResponseWriter, response any) error {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("response writer does not support streaming")
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	sequenceNumber := int64(1)
	send := func(event any) error {
		payload, err := json.Marshal(event)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}

	createdResponse := createdResponseEventPayload(response)
	if err := send(map[string]any{
		"type":            "response.created",
		"sequence_number": sequenceNumber,
		"response":        createdResponse,
	}); err != nil {
		return err
	}
	sequenceNumber++

	if err := streamResponseOutput(send, response, &sequenceNumber); err != nil {
		return err
	}

	completedResponse := completedResponseEventPayload(response)
	if err := send(map[string]any{
		"type":            "response.completed",
		"sequence_number": sequenceNumber,
		"response":        completedResponse,
	}); err != nil {
		return err
	}

	if _, err := io.WriteString(w, "data: [DONE]\n\n"); err != nil {
		return err
	}
	flusher.Flush()

	return nil
}

// The streamResponseOutput function streams supported output items from a decoded response payload.
func streamResponseOutput(send func(any) error, response any, sequenceNumber *int64) error {
	responseObject, ok := response.(map[string]any)
	if !ok {
		return nil
	}

	rawOutput, ok := responseObject["output"].([]any)
	if !ok {
		return nil
	}

	for outputIndex, rawItem := range rawOutput {
		item, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}

		itemType, _ := item["type"].(string)
		itemID, _ := item["id"].(string)

		switch itemType {
		case "message":
			if err := streamMessageOutput(send, itemID, outputIndex, item, sequenceNumber); err != nil {
				return err
			}
		case "function_call":
			if err := streamFunctionCall(send, itemID, outputIndex, item, sequenceNumber); err != nil {
				return err
			}
		case "custom_tool_call":
			if err := streamCustomToolCall(send, itemID, outputIndex, item, sequenceNumber); err != nil {
				return err
			}
		}
	}

	return nil
}

// The streamMessageOutput function streams text deltas and completion events for a message output item.
func streamMessageOutput(send func(any) error, itemID string, outputIndex int, item map[string]any, sequenceNumber *int64) error {
	rawContent, ok := item["content"].([]any)
	if !ok {
		return nil
	}

	for contentIndex, rawPart := range rawContent {
		part, ok := rawPart.(map[string]any)
		if !ok {
			continue
		}
		if part["type"] != "output_text" {
			continue
		}

		text, _ := part["text"].(string)
		for _, chunk := range splitText(text) {
			if err := send(map[string]any{
				"type":            "response.output_text.delta",
				"sequence_number": *sequenceNumber,
				"item_id":         itemID,
				"output_index":    outputIndex,
				"content_index":   contentIndex,
				"delta":           chunk,
				"logprobs":        []any{},
			}); err != nil {
				return err
			}
			*sequenceNumber++
		}

		if err := send(map[string]any{
			"type":            "response.output_text.done",
			"sequence_number": *sequenceNumber,
			"item_id":         itemID,
			"output_index":    outputIndex,
			"content_index":   contentIndex,
			"text":            text,
			"logprobs":        []any{},
		}); err != nil {
			return err
		}
		*sequenceNumber++
	}

	return nil
}

// The streamFunctionCall function streams argument deltas and completion events for a function_call output item.
func streamFunctionCall(send func(any) error, itemID string, outputIndex int, item map[string]any, sequenceNumber *int64) error {
	arguments, _ := item["arguments"].(string)
	name, _ := item["name"].(string)

	for _, chunk := range splitText(arguments) {
		if err := send(map[string]any{
			"type":            "response.function_call_arguments.delta",
			"sequence_number": *sequenceNumber,
			"item_id":         itemID,
			"output_index":    outputIndex,
			"delta":           chunk,
		}); err != nil {
			return err
		}
		*sequenceNumber++
	}

	if arguments != "" || name != "" {
		if err := send(map[string]any{
			"type":            "response.function_call_arguments.done",
			"sequence_number": *sequenceNumber,
			"item_id":         itemID,
			"output_index":    outputIndex,
			"name":            name,
			"arguments":       arguments,
		}); err != nil {
			return err
		}
		*sequenceNumber++
	}

	return streamOutputItemDone(send, outputIndex, completedOutputItem(item), sequenceNumber)
}

// The streamCustomToolCall function streams input deltas and completion events for a custom_tool_call output item.
func streamCustomToolCall(send func(any) error, itemID string, outputIndex int, item map[string]any, sequenceNumber *int64) error {
	input, _ := item["input"].(string)

	for _, chunk := range splitText(input) {
		if err := send(map[string]any{
			"type":            "response.custom_tool_call_input.delta",
			"sequence_number": *sequenceNumber,
			"item_id":         itemID,
			"output_index":    outputIndex,
			"delta":           chunk,
		}); err != nil {
			return err
		}
		*sequenceNumber++
	}

	if input != "" {
		if err := send(map[string]any{
			"type":            "response.custom_tool_call_input.done",
			"sequence_number": *sequenceNumber,
			"item_id":         itemID,
			"output_index":    outputIndex,
			"input":           input,
		}); err != nil {
			return err
		}
		*sequenceNumber++
	}

	return streamOutputItemDone(send, outputIndex, completedOutputItem(item), sequenceNumber)
}

func streamOutputItemDone(send func(any) error, outputIndex int, item map[string]any, sequenceNumber *int64) error {
	if err := send(map[string]any{
		"type":            "response.output_item.done",
		"sequence_number": *sequenceNumber,
		"output_index":    outputIndex,
		"item":            item,
	}); err != nil {
		return err
	}
	*sequenceNumber++

	return nil
}

func createdResponseEventPayload(response any) any {
	responseObject, ok := response.(map[string]any)
	if !ok {
		return response
	}

	created := cloneObject(responseObject)
	created["status"] = "in_progress"
	created["output"] = []any{}
	created["usage"] = nil
	return created
}

func completedResponseEventPayload(response any) any {
	responseObject, ok := response.(map[string]any)
	if !ok {
		return response
	}

	status, _ := responseObject["status"].(string)
	if status != "" {
		return response
	}

	completed := cloneObject(responseObject)
	completed["status"] = "completed"
	return completed
}

func completedOutputItem(item map[string]any) map[string]any {
	itemType, _ := item["type"].(string)
	if itemType != "function_call" {
		return item
	}

	status, _ := item["status"].(string)
	if status != "" {
		return item
	}

	completed := cloneObject(item)
	completed["status"] = "completed"
	return completed
}

func cloneObject(source map[string]any) map[string]any {
	cloned := make(map[string]any, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
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

// The splitText function splits text into UTF-8-safe chunks for streaming.
func splitText(text string) []string {
	if text == "" {
		return nil
	}
	if utf8.RuneCountInString(text) <= chunkRunes {
		return []string{text}
	}

	chunks := make([]string, 0, (utf8.RuneCountInString(text)/chunkRunes)+1)
	start := 0
	runeCount := 0

	for index := range text {
		if runeCount == chunkRunes {
			chunks = append(chunks, text[start:index])
			start = index
			runeCount = 0
		}
		runeCount++
	}

	chunks = append(chunks, text[start:])
	return chunks
}

// The stripComments function removes JSONC line and block comments that appear outside string literals.
func stripComments(data []byte) []byte {
	result := make([]byte, 0, len(data))
	inString := false
	escaped := false
	inLineComment := false
	inBlockComment := false

	for i := 0; i < len(data); i++ {
		current := data[i]

		if inLineComment {
			if current == '\n' {
				inLineComment = false
				result = append(result, current)
			}
			continue
		}

		if inBlockComment {
			if current == '\n' {
				result = append(result, current)
			}
			if current == '*' && i+1 < len(data) && data[i+1] == '/' {
				inBlockComment = false
				i++
			}
			continue
		}

		if inString {
			result = append(result, current)
			if escaped {
				escaped = false
				continue
			}
			if current == '\\' {
				escaped = true
				continue
			}
			if current == '"' {
				inString = false
			}
			continue
		}

		if current == '"' {
			inString = true
			result = append(result, current)
			continue
		}

		if current == '/' && i+1 < len(data) {
			switch data[i+1] {
			case '/':
				inLineComment = true
				i++
				continue
			case '*':
				inBlockComment = true
				i++
				continue
			}
		}

		result = append(result, current)
	}

	return result
}

// The stripTrailingCommas function removes JSONC trailing commas that appear outside string literals.
func stripTrailingCommas(data []byte) []byte {
	result := make([]byte, 0, len(data))
	inString := false
	escaped := false

	for i := 0; i < len(data); i++ {
		current := data[i]

		if inString {
			result = append(result, current)
			if escaped {
				escaped = false
				continue
			}
			if current == '\\' {
				escaped = true
				continue
			}
			if current == '"' {
				inString = false
			}
			continue
		}

		if current == '"' {
			inString = true
			result = append(result, current)
			continue
		}

		if current == ',' {
			j := i + 1
			for j < len(data) {
				switch data[j] {
				case ' ', '\n', '\r', '\t':
					j++
				default:
					goto nextToken
				}
			}

		nextToken:
			if j < len(data) && (data[j] == '}' || data[j] == ']') {
				continue
			}
		}

		result = append(result, current)
	}

	return result
}
