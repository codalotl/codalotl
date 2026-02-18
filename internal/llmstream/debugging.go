package llmstream

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/openai/openai-go/v3/responses"
)

//
// I often want to quickly debug LLMs by seeing HTTP inputs/outputs. Even though/if there's a way to see this stuff with something like SetLogger(logger *slog.Logger) on Conversation,
// it's often annoying to figure out how to do that and thread it through in the proper places.
//
// As such, we can enable printing of this info on an ad-hoc basis by setting these local vars and running the test/program. Can easily set to os.Stdout to print to standard out.
//

var (
	// Shows HTTP requests
	debugHTTPRequests *os.File

	// Shows primary HTTP responses (ex: final response object raw JSON from server).
	debugHTTPResponses *os.File

	// Shows primary parsed objects (ex: new assistant message being added; final Response in the EventTypeCompletedSuccess event).
	debugParsedResponses *os.File

	debugTools  *os.File // Shows tool calls and resuls
	debugMisc   *os.File // Log Misc things not cleanly in any other bucket
	debugEvents *os.File // Log Misc things not cleanly in any other bucket
)

// NOTE: could add debugRawEvents, to show all the deltas and stuff

func init() {
	// Silence unused errors:
	_ = debugHTTPRequests
	_ = debugHTTPResponses
	_ = debugParsedResponses
	_ = debugMisc
	_ = debugTools
	_ = debugEvents

	var output *os.File

	if logFilePath := os.Getenv("LLMSTREAM_LOG_FILE"); logFilePath != "" {
		dir := filepath.Dir(logFilePath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			panic(err)
		}
		f, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			panic(err)
		}
		output = f
	} else {
		// output = os.Stdout
	}

	debugHTTPRequests = output
	debugHTTPResponses = output
	debugParsedResponses = output
	debugMisc = output

	// NOTE: Tool output is separate, since I often want to look at that separately:
	var toolsOutput *os.File
	if logFilePath := os.Getenv("LLMSTREAM_TOOLS_LOG_FILE"); logFilePath != "" {
		dir := filepath.Dir(logFilePath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			panic(err)
		}
		f, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			panic(err)
		}
		toolsOutput = f
	} else {
		// toolsOutput = os.Stdout
	}
	debugTools = toolsOutput

	// NOTE: Tool output is separate, since I often want to look at that separately:
	var eventsOutput *os.File
	if logFilePath := os.Getenv("LLMSTREAM_EVENTS_LOG_FILE"); logFilePath != "" {
		dir := filepath.Dir(logFilePath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			panic(err)
		}
		f, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			panic(err)
		}
		eventsOutput = f
	} else {
		// debugEvents = os.Stdout
	}
	debugEvents = eventsOutput
}

func debugPrint(file *os.File, msg string, obj any) {
	if file != nil {
		if obj == nil {
			fmt.Fprintf(file, "== DEBUG: %s (nil)\n", msg)
			return
		}
		fmt.Fprintf(file, "== DEBUG: %s\n", msg)

		// if obj is a string, try to see if it's really json. if it is, pretty print it.
		// if obj is a non-json string, just print it
		// if obj is has Error() string, print the string
		// if obj can be printed with MarshalIndent, then do that
		// if obj has String() string, use that
		// otherwise just print it with %v and hope for the best
		if s, ok := obj.(string); ok {
			var out bytes.Buffer
			if err := json.Indent(&out, []byte(s), "", "  "); err == nil {
				_, _ = debugHTTPRequests.Write(out.Bytes())
				_, _ = debugHTTPRequests.Write([]byte("\n"))
				return
			}
			fmt.Fprintln(file, s)
			return
		}

		if err, ok := obj.(error); ok {
			fmt.Fprintln(file, err.Error())
			return
		}

		if b, err := json.MarshalIndent(obj, "", "  "); err == nil {
			_, _ = file.Write(b)
			_, _ = file.Write([]byte("\n"))
			return
		}

		if err, ok := obj.(fmt.Stringer); ok {
			fmt.Fprintln(file, err.String())
			return
		}

		fmt.Println(file, obj)
	}
}

// Example:
//
//	Tool Call: call_id=call_1234 name=ls type=function_call
//	  {"path": "some/path"}
//	->
//	  foo/
//	  bar/
//	  file.txt
//
// Notes:
//   - doesn't print out provide id
//   - doesn't duplicate call id/name/type (but WARNS if they're different)
//   - if output is just text,print that.
//   - If it's serialized JSON, print it, BUT:
//   - if there's a "content" field, replace it with "V" and then print the text of the content on the following line
//   - indend input JSON and output text by 2 spaces on every line
func debugPrintToolCallResult(call ToolCall, result ToolResult) {
	if debugTools == nil {
		return
	}

	var buf bytes.Buffer

	fmt.Fprintf(&buf, "Tool Call: call_id=%s name=%s type=%s\n", call.CallID, call.Name, call.Type)

	toolResultMismatches := func() []string {
		var mismatches []string
		if result.CallID != "" && result.CallID != call.CallID {
			mismatches = append(mismatches, fmt.Sprintf("call_id=%s", result.CallID))
		}
		if result.Name != "" && result.Name != call.Name {
			mismatches = append(mismatches, fmt.Sprintf("name=%s", result.Name))
		}
		if result.Type != "" && result.Type != call.Type {
			mismatches = append(mismatches, fmt.Sprintf("type=%s", result.Type))
		}
		return mismatches
	}

	writeIndentedBlock := func(buf *bytes.Buffer, text string) {
		if text == "" {
			return
		}
		lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
		for _, line := range lines {
			buf.WriteString("  ")
			buf.WriteString(line)
			buf.WriteByte('\n')
		}
	}

	if mismatches := toolResultMismatches(); len(mismatches) > 0 {
		fmt.Fprintf(&buf, "  WARN: tool result mismatch (%s)\n", strings.Join(mismatches, ", "))
	}

	if formattedInput := debugFormatToolJSON(call.Input); formattedInput != "" {
		writeIndentedBlock(&buf, formattedInput)
	} else {
		writeIndentedBlock(&buf, "(no input)")
	}

	buf.WriteString("->\n")

	if formattedResult, contentText, isJSON := debugFormatToolResult(result.Result); isJSON {
		if formattedResult != "" {
			writeIndentedBlock(&buf, formattedResult)
		}
		if contentText != "" {
			writeIndentedBlock(&buf, contentText)
		}
	} else if trimmed := strings.TrimSpace(result.Result); trimmed != "" {
		writeIndentedBlock(&buf, trimmed)
	} else {
		writeIndentedBlock(&buf, "(empty result)")
	}

	buf.WriteByte('\n')

	_, _ = debugTools.Write(buf.Bytes())
}

func debugFormatToolJSON(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	if !json.Valid([]byte(normalized)) {
		return normalized
	}
	var obj any
	if err := json.Unmarshal([]byte(normalized), &obj); err != nil {
		return normalized
	}
	pretty, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return normalized
	}
	return string(pretty)
}

func debugFormatToolResult(raw string) (formatted string, content string, isJSON bool) {
	if strings.TrimSpace(raw) == "" {
		return "", "", false
	}

	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	if !json.Valid([]byte(normalized)) {
		return "", "", false
	}

	var obj any
	if err := json.Unmarshal([]byte(normalized), &obj); err != nil {
		return "", "", false
	}

	switch v := obj.(type) {
	case map[string]any:
		var (
			contentText string
			hasContent  bool
		)
		if c, ok := v["content"].(string); ok {
			contentText = c
			v["content"] = "V"
			hasContent = true
		}
		pretty, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return "", "", false
		}
		if hasContent {
			return string(pretty), contentText, true
		}
		return string(pretty), "", true
	default:
		pretty, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return "", "", false
		}
		return string(pretty), "", true
	}
}

// Returns a concise single-line description of an OpenAI Responses response.output_item.added event's item to avoid dumping the full union.
func debugDescribeOutputItemAdded(evt responses.ResponseOutputItemAddedEvent) string {
	item := evt.Item
	switch item.Type {
	case "function_call":
		fn := item.AsFunctionCall()
		return fmt.Sprintf("ADDED: type=function_call id=%s name=%s call_id=%s", item.ID, fn.Name, fn.CallID)
	case "custom_tool_call":
		custom := item.AsCustomToolCall()
		return fmt.Sprintf("ADDED: type=custom_tool_call id=%s name=%s call_id=%s", item.ID, custom.Name, custom.CallID)
	case "message":
		msg := item.AsMessage()
		return fmt.Sprintf("ADDED: type=message id=%s contents=%d", msg.ID, len(msg.Content))
	case "reasoning":
		reason := item.AsReasoning()
		return fmt.Sprintf("ADDED: type=reasoning id=%s summaries=%d", reason.ID, len(reason.Summary))
	default:
		return fmt.Sprintf("ADDED: type=%s id=%s", item.Type, item.ID)
	}
}
