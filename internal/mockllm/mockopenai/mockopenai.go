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

type rawConfig struct {
	Responses []rawResponse `json:"responses"`
}

type rawResponse struct {
	Name     string                     `json:"name"`
	Consume  bool                       `json:"consume"`
	Request  map[string]json.RawMessage `json:"request"`
	Headers  []rawHeader                `json:"headers"`
	Response json.RawMessage            `json:"response"`
}

type rawHeader struct {
	Name  string          `json:"name"`
	Value json.RawMessage `json:"value"`
}

type compiledResponse struct {
	name            string
	consume         bool
	requestMatchers map[string]valueMatcher
	headerMatchers  []headerMatcher
	response        any
	consumed        bool
}

type headerMatcher struct {
	name  string
	value valueMatcher
}

type valueMatcher struct {
	matchType  string
	text       string
	hasLiteral bool
	literal    string
}

type handler struct {
	mu        sync.Mutex
	responses []compiledResponse
}

// NewHandlerFromFile creates an http.Handler that serves mock OpenAI Responses API requests using response definitions loaded from a JSON or JSON-with-comments
// file.
func NewHandlerFromFile(path string) (http.Handler, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read mock OpenAI responses file: %w", err)
	}

	return NewHandler(data)
}

// NewHandler creates an http.Handler that serves mock OpenAI Responses API requests using response definitions loaded from JSON or JSON-with-comments bytes.
func NewHandler(data []byte) (http.Handler, error) {
	responses, err := parseConfig(data)
	if err != nil {
		return nil, err
	}

	return &handler{responses: responses}, nil
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
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		return valueMatcher{
			matchType: matchExact,
			text:      text,
		}, nil
	}

	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err == nil && len(object) > 0 {
		if rawText, ok := object["text"]; ok {
			if len(object) > 2 {
				return valueMatcher{}, fmt.Errorf("text matcher supports only %q and %q fields", "match", "text")
			}

			if err := json.Unmarshal(rawText, &text); err != nil {
				return valueMatcher{}, fmt.Errorf("parse text matcher text: %w", err)
			}

			matchType := matchExact
			if rawMatchType, ok := object["match"]; ok {
				if err := json.Unmarshal(rawMatchType, &matchType); err != nil {
					return valueMatcher{}, fmt.Errorf("parse text matcher match type: %w", err)
				}
			}

			switch matchType {
			case matchExact, matchPartial:
				return valueMatcher{
					matchType: matchType,
					text:      text,
				}, nil
			default:
				return valueMatcher{}, fmt.Errorf("unsupported match type %q", matchType)
			}
		}
	}

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

		return response.response, true
	}

	return nil, false
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

func (m valueMatcher) matches(actual any) bool {
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

	switch m.matchType {
	case matchExact:
		return actualText == m.text
	case matchPartial:
		return strings.Contains(actualText, m.text)
	default:
		return false
	}
}

func actualMatchText(actual any) (string, bool) {
	if text, ok := actual.(string); ok {
		return text, true
	}

	encoded, err := json.Marshal(actual)
	if err != nil {
		return "", false
	}

	return string(encoded), true
}

func canonicalJSON(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}

	return string(encoded), nil
}

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
