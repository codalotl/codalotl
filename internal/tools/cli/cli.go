// Package cli provides the codalotl_cli LLM tool, an in-process wrapper around a caller-supplied, whitelisted codalotl command tree.
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/llmstream"
	qcli "github.com/codalotl/codalotl/internal/q/cli"
)

// ToolNameCodalotlCLI is the LLM tool name for the in-process codalotl CLI wrapper.
const ToolNameCodalotlCLI = "codalotl_cli"

const (
	visibleOutputMaxLineRunes  = 1200
	visibleOutputMaxChunkBytes = 16 * 1024
	visibleOutputMaxTotalBytes = 64 * 1024
)

var (
	emitToolOutput                = agent.EmitToolOutput
	visibleOutputNewlineFlushWait = 75 * time.Millisecond
	visibleOutputPartialFlushWait = time.Second
)

// CommandTreeFunc returns a fresh whitelisted codalotl command tree.
type CommandTreeFunc func() *qcli.Command

// NewCodalotlCLITool creates the codalotl_cli tool.
//
// The returned tool captures command stdout and stderr in Result. When its Run context supports display-only tool output, Run also streams command stdout visibly
// while the command runs; this applies to direct Run calls as well as agent-runtime tool invocations. Stderr is captured but not visibly streamed.
func NewCodalotlCLITool(newCommandTree CommandTreeFunc) llmstream.Tool {
	return &codalotlCLITool{newCommandTree: newCommandTree}
}

// Params are the codalotl_cli tool parameters.
type Params struct {
	Subcommand string   `json:"subcommand"` // Subcommand is the command path after codalotl (ex: "context initial"); flags and positional args belong in Argv.
	Argv       []string `json:"argv"`       // Argv is the exact flag and positional argument vector for Subcommand; nil is treated as empty.
}

// Result is the machine-readable codalotl_cli tool result.
type Result struct {
	Success  bool     `json:"success"`   // Success reports whether ExitCode is 0.
	Command  []string `json:"command"`   // Command is the command vector, starting with "codalotl".
	ExitCode int      `json:"exit_code"` // ExitCode is the process-style exit code returned by the command.
	Stdout   string   `json:"stdout"`    // Stdout is the command's captured standard output.
	Stderr   string   `json:"stderr"`    // Stderr is the command's captured standard error.
}

// codalotlCLITool implements llmstream.Tool by running a whitelisted q/cli command tree in-process.
type codalotlCLITool struct {
	newCommandTree CommandTreeFunc // The factory is called for each run to obtain an isolated whitelisted command tree.
}

// Info returns the LLM-visible metadata and parameter schema for codalotl_cli.
func (t *codalotlCLITool) Info() llmstream.ToolInfo {
	return llmstream.ToolInfo{
		Name: ToolNameCodalotlCLI,
		Description: "Run whitelisted codalotl CLI commands in-process. " +
			"Use subcommand \"help\" or \"--help\" to list available commands. " +
			"Pass command flags and positional args in argv; pass per-command --help in argv for detailed help.",
		Parameters: map[string]any{
			"subcommand": map[string]any{
				"type":        "string",
				"description": "Command path after codalotl, such as \"context initial\" or \"docs add\". Use \"help\" or \"--help\" to list available commands.",
			},
			"argv": map[string]any{
				"type":        []string{"array", "null"},
				"description": "Flags and args for the subcommand. Null behaves like an empty array.",
				"items": map[string]any{
					"type": "string",
				},
			},
		},
		Required: []string{"subcommand", "argv"},
	}
}

// Name returns ToolNameCodalotlCLI.
func (t *codalotlCLITool) Name() string {
	return ToolNameCodalotlCLI
}

// Presenter returns the semantic presenter used for codalotl_cli status lines.
func (t *codalotlCLITool) Presenter() llmstream.Presenter {
	return codalotlCLIPresenter{}
}

// Run executes a codalotl_cli tool call against a fresh whitelisted command tree. It parses call.Input as Params, captures stdout and stderr, streams stdout visibly
// when supported, and propagates ctx to command handlers. Malformed parameters and command-tree construction failures are returned as tool infrastructure errors;
// command usage errors and non-zero exits are encoded in Result.
func (t *codalotlCLITool) Run(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
	params, err := parseParams(call.Input)
	if err != nil {
		return errorToolResult(call, fmt.Sprintf("malformed %s params: %v", ToolNameCodalotlCLI, err), err)
	}

	tokens := strings.Fields(params.Subcommand)
	command := commandVector(tokens, params.Argv)
	if len(tokens) == 0 {
		return jsonToolResult(call, Result{
			Success:  false,
			Command:  command,
			ExitCode: 2,
			Stderr:   "usage error: empty subcommand\n",
		})
	}

	root, err := t.freshCommandTree()
	if err != nil {
		return errorToolResult(call, err.Error(), err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	out := newStreamingStdoutWriter(ctx, &stdout)
	if isCatalogHelp(tokens) {
		qcli.WriteHelp(out, root, root, qcli.HelpOptions{LeafCommands: true})
		out.Close()
		return jsonToolResult(call, Result{
			Success:  true,
			Command:  command,
			ExitCode: 0,
			Stdout:   stdout.String(),
			Stderr:   stderr.String(),
		})
	}

	args := append([]string(nil), tokens...)
	args = append(args, params.Argv...)
	exitCode := qcli.Run(ctx, root, qcli.Options{
		Args: args,
		Out:  out,
		Err:  &stderr,
	})
	out.Close()

	return jsonToolResult(call, Result{
		Success:  exitCode == 0,
		Command:  command,
		ExitCode: exitCode,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
	})
}

// freshCommandTree returns a new command tree rooted at the public codalotl command name. It reports nil factories, factory panics, and nil roots as errors.
func (t *codalotlCLITool) freshCommandTree() (root *qcli.Command, err error) {
	if t.newCommandTree == nil {
		return nil, errors.New("codalotl_cli command tree factory is nil")
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("codalotl_cli command tree factory panicked: %v", recovered)
		}
	}()
	root = t.newCommandTree()
	if root == nil {
		return nil, errors.New("codalotl_cli command tree factory returned nil")
	}
	root.Name = "codalotl"
	return root, nil
}

// parseParams decodes a single JSON object into Params. It requires subcommand and argv, rejects unknown fields and extra JSON values, and treats a null argv as
// nil.
func parseParams(input string) (Params, error) {
	var raw map[string]json.RawMessage
	decoder := json.NewDecoder(strings.NewReader(input))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		return Params{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Params{}, errors.New("multiple JSON values")
	}

	for key := range raw {
		if key != "subcommand" && key != "argv" {
			return Params{}, fmt.Errorf("unknown field %q", key)
		}
	}
	if _, ok := raw["subcommand"]; !ok {
		return Params{}, errors.New("missing required field \"subcommand\"")
	}
	if _, ok := raw["argv"]; !ok {
		return Params{}, errors.New("missing required field \"argv\"")
	}

	var params Params
	if err := json.Unmarshal(raw["subcommand"], &params.Subcommand); err != nil {
		return Params{}, fmt.Errorf("subcommand: %w", err)
	}
	if string(raw["argv"]) != "null" {
		if err := json.Unmarshal(raw["argv"], &params.Argv); err != nil {
			return Params{}, fmt.Errorf("argv: %w", err)
		}
	}
	return params, nil
}

func isCatalogHelp(tokens []string) bool {
	return len(tokens) == 1 && (tokens[0] == "help" || tokens[0] == "--help")
}

func commandVector(subcommandTokens []string, argv []string) []string {
	command := []string{"codalotl"}
	command = append(command, subcommandTokens...)
	command = append(command, argv...)
	return command
}

func jsonToolResult(call llmstream.ToolCall, result Result) llmstream.ToolResult {
	body, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return errorToolResult(call, fmt.Sprintf("failed to encode %s result: %v", ToolNameCodalotlCLI, err), err)
	}
	return llmstream.ToolResult{
		CallID:  call.CallID,
		Name:    call.Name,
		Type:    call.Type,
		Result:  string(body),
		IsError: false,
	}
}

func errorToolResult(call llmstream.ToolCall, msg string, srcErr error) llmstream.ToolResult {
	return llmstream.ToolResult{
		CallID:    call.CallID,
		Name:      call.Name,
		Type:      call.Type,
		Result:    msg,
		IsError:   true,
		SourceErr: srcErr,
	}
}

// codalotlCLIPresenter presents codalotl_cli calls as concise running and completed command summaries.
type codalotlCLIPresenter struct{}

// Present returns a replace-style summary for call. It uses "Running" before a result is available and "Ran" after completion, formatting the command as a shell
// command when the input can be parsed.
func (codalotlCLIPresenter) Present(call llmstream.ToolCall, result *llmstream.ToolResult) llmstream.Presentation {
	action := "Running"
	if result != nil {
		action = "Ran"
	}
	return llmstream.Presentation{
		Behavior: llmstream.CompletionBehaviorReplace,
		Summary: llmstream.Line{
			Segments: []llmstream.Segment{{
				Text: action + " " + presentationCommand(call),
				Role: llmstream.RoleAction,
			}},
		},
	}
}

func presentationCommand(call llmstream.ToolCall) string {
	params, err := parseParams(call.Input)
	if err != nil {
		return ToolNameCodalotlCLI
	}
	return shellCommandString(commandVector(strings.Fields(params.Subcommand), params.Argv))
}

func shellCommandString(command []string) string {
	parts := make([]string, 0, len(command))
	for _, part := range command {
		parts = append(parts, shellQuote(part))
	}
	return strings.Join(parts, " ")
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\r\n'\"\\$`|&;<>(){}[]*?!") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// streamingStdoutWriter captures stdout while mirroring it to display-only visible output.
type streamingStdoutWriter struct {
	capture io.Writer              // The capture writer receives stdout bytes for Result.Stdout.
	stream  *visibleOutputStreamer // The stream emits sanitized display-only stdout chunks.
}

func newStreamingStdoutWriter(ctx context.Context, capture io.Writer) *streamingStdoutWriter {
	return &streamingStdoutWriter{
		capture: capture,
		stream:  newVisibleOutputStreamer(ctx, emitToolOutput),
	}
}

// Write writes p to the capture writer and mirrors the successfully captured prefix to the visible output stream. It returns the byte count and error reported by
// the capture writer.
func (w *streamingStdoutWriter) Write(p []byte) (int, error) {
	n, err := w.capture.Write(p)
	if n > 0 {
		w.stream.Write(p[:n])
	}
	return n, err
}

// Close flushes and closes the visible output stream. It does not close the capture writer.
func (w *streamingStdoutWriter) Close() {
	w.stream.Close()
}

// visibleFlushMode selects how much pending visible output a flush may emit.
type visibleFlushMode int

const (
	visibleFlushAll visibleFlushMode = iota
	visibleFlushNewline
)

// visibleOutputStreamer buffers stdout and emits sanitized, bounded visible-output chunks.
type visibleOutputStreamer struct {
	ctx             context.Context               // The context is passed to the visible-output emitter.
	emit            func(context.Context, string) // The emitter receives display-only stdout chunks; nil disables visible emission.
	mu              sync.Mutex                    // The mutex protects all mutable fields below.
	pending         []byte                        // Pending stores raw bytes waiting for a scheduled or final flush.
	timer           *time.Timer                   // Timer triggers the next scheduled flush, when one is pending.
	timerGeneration uint64                        // The timer generation invalidates callbacks from canceled or replaced timers.
	closed          bool                          // Closed records whether the streamer has been closed.
	emittedBytes    int                           // The emitted byte count tracks visible content prepared against the total output budget.
	elided          bool                          // Elided records whether the total visible-output budget has been exhausted.
}

func newVisibleOutputStreamer(ctx context.Context, emit func(context.Context, string)) *visibleOutputStreamer {
	return &visibleOutputStreamer{ctx: ctx, emit: emit}
}

// Write buffers raw stdout bytes for visible emission and schedules a flush. Newline-containing buffers are flushed soon as complete-line content; other partial
// buffers are flushed after the partial-output delay. Write ignores empty input, writes after Close, and writes after the stream has been elided.
func (s *visibleOutputStreamer) Write(p []byte) {
	if len(p) == 0 {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed || s.elided {
		return
	}
	s.pending = append(s.pending, p...)
	if bytes.LastIndexByte(s.pending, '\n') >= 0 {
		s.scheduleLocked(visibleOutputNewlineFlushWait, visibleFlushNewline)
		return
	}
	if s.timer == nil {
		s.scheduleLocked(visibleOutputPartialFlushWait, visibleFlushAll)
	}
}

// Close emits all pending visible output and prevents future emission. It is safe to call more than once.
func (s *visibleOutputStreamer) Close() {
	content := s.close()
	s.emitContent(content)
}

// The close helper marks s closed, cancels scheduled flushes, and returns all remaining visible content for the caller to emit. It is idempotent and returns an
// empty string after the first close.
func (s *visibleOutputStreamer) close() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ""
	}
	s.closed = true
	s.timerGeneration++
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	return s.takeLocked(visibleFlushAll)
}

// scheduleLocked schedules a future flush and invalidates any previously scheduled flush. The caller must hold s.mu.
func (s *visibleOutputStreamer) scheduleLocked(wait time.Duration, mode visibleFlushMode) {
	s.timerGeneration++
	generation := s.timerGeneration
	if s.timer != nil {
		s.timer.Stop()
	}
	s.timer = time.AfterFunc(wait, func() {
		s.flushScheduled(generation, mode)
	})
}

// flushScheduled runs a scheduled flush and emits any content it produces.
func (s *visibleOutputStreamer) flushScheduled(generation uint64, mode visibleFlushMode) {
	content := s.flush(generation, mode)
	s.emitContent(content)
}

// flush processes a scheduled flush for generation and returns content to emit. It ignores stale callbacks and closed streams, and it reschedules another flush
// when pending content remains.
func (s *visibleOutputStreamer) flush(generation uint64, mode visibleFlushMode) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed || generation != s.timerGeneration {
		return ""
	}
	s.timer = nil
	content := s.takeLocked(mode)
	if len(s.pending) == 0 || s.elided {
		return content
	}
	if bytes.LastIndexByte(s.pending, '\n') >= 0 {
		s.scheduleLocked(visibleOutputNewlineFlushWait, visibleFlushNewline)
	} else {
		s.scheduleLocked(visibleOutputPartialFlushWait, visibleFlushAll)
	}
	return content
}

// takeLocked removes pending bytes selected by mode, prepares them for visible emission, and returns the content to emit. The caller must hold s.mu. It returns
// an empty string when there is no flushable content.
func (s *visibleOutputStreamer) takeLocked(mode visibleFlushMode) string {
	if len(s.pending) == 0 || s.elided {
		return ""
	}

	n := len(s.pending)
	if mode == visibleFlushNewline {
		idx := bytes.LastIndexByte(s.pending, '\n')
		if idx < 0 {
			return ""
		}
		n = idx + 1
	}

	raw := string(s.pending[:n])
	s.pending = append([]byte(nil), s.pending[n:]...)
	return s.prepareVisibleContentLocked(raw)
}

// prepareVisibleContentLocked sanitizes raw output and enforces visible-output chunk and total byte limits. The caller must hold s.mu. It returns an empty string
// for content that becomes empty after sanitization and marks the stream elided when the total budget is exhausted.
func (s *visibleOutputStreamer) prepareVisibleContentLocked(raw string) string {
	content := sanitizeVisibleOutput(raw)
	if content == "" {
		return ""
	}

	if len(content) > visibleOutputMaxChunkBytes {
		content = truncateStringBytes(content, visibleOutputMaxChunkBytes) + "\n... visible output chunk elided ...\n"
	}

	remaining := visibleOutputMaxTotalBytes - s.emittedBytes
	if remaining <= 0 {
		s.elided = true
		return "... visible output elided ...\n"
	}
	if len(content) > remaining {
		content = truncateStringBytes(content, remaining) + "\n... visible output elided ...\n"
		s.elided = true
	}
	s.emittedBytes += len(content)
	return content
}

// emitContent sends non-empty visible output to the configured emitter. It does nothing when content is empty or no emitter is configured.
func (s *visibleOutputStreamer) emitContent(content string) {
	if content == "" || s.emit == nil {
		return
	}
	s.emit(s.ctx, content)
}

// sanitizeVisibleOutput converts raw command output into display-safe text. It normalizes invalid UTF-8, strips ANSI sequences, normalizes carriage returns, expands
// tabs, replaces control characters, and elides overly long lines.
func sanitizeVisibleOutput(raw string) string {
	raw = strings.ToValidUTF8(raw, "?")
	raw = stripANSISequences(raw)
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\r", "\n")

	var out strings.Builder
	lineLen := 0
	lineElided := false
	for _, r := range raw {
		switch {
		case r == '\n':
			out.WriteByte('\n')
			lineLen = 0
			lineElided = false
		case r == '\t':
			writeVisibleToken(&out, "    ", &lineLen, &lineElided)
		case r < 0x20 || r == 0x7f:
			writeVisibleToken(&out, "?", &lineLen, &lineElided)
		default:
			writeVisibleToken(&out, string(r), &lineLen, &lineElided)
		}
	}
	return out.String()
}

func writeVisibleToken(out *strings.Builder, token string, lineLen *int, lineElided *bool) {
	for _, r := range token {
		if *lineLen < visibleOutputMaxLineRunes {
			out.WriteRune(r)
			*lineLen = *lineLen + 1
			continue
		}
		if !*lineElided {
			out.WriteString("...")
			*lineElided = true
		}
	}
}

// stripANSISequences removes ANSI CSI escape sequences from s and replaces unsupported escape bytes with "?".
func stripANSISequences(s string) string {
	var out strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '\x1b' {
			r, size := utf8.DecodeRuneInString(s[i:])
			out.WriteRune(r)
			i += size
			continue
		}

		if i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) {
				c := s[i]
				i++
				if c >= 0x40 && c <= 0x7e {
					break
				}
			}
			continue
		}

		out.WriteByte('?')
		i++
	}
	return out.String()
}

func truncateStringBytes(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	if maxBytes <= 0 {
		return ""
	}
	for maxBytes > 0 && !utf8.RuneStart(s[maxBytes]) {
		maxBytes--
	}
	return s[:maxBytes]
}
