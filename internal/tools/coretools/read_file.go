package coretools

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
)

//go:embed read_file.md
var descriptionReadFile string

// A toolReadFile implements the read_file tool for returning authorized file contents within the package read limits.
type toolReadFile struct {
	sandboxAbsDir string                // This is the absolute sandbox root used to resolve relative read paths.
	authorizer    authdomain.Authorizer // This authorizes read requests before files are opened.
}

// ParamsReadFile contains the JSON arguments for the read_file tool.
type ParamsReadFile struct {
	Path              string `json:"path"`               // Path is the file to read. Relative paths are resolved from the sandbox root.
	LineNumbers       bool   `json:"line_numbers"`       // LineNumbers reports whether output lines should include 1-based line number prefixes.
	RequestPermission bool   `json:"request_permission"` // RequestPermission asks for approval to read the file when policy requires it.
}

const (
	ToolNameReadFile         = "read_file" // ToolNameReadFile is the registered name of the read_file tool.
	maxReadFileBytes   int64 = 250 * 1024  // 250KB
	maxReadFileLines   int   = 10000
	maxLineLengthChars int   = 2000
)

// NewReadFileTool returns a read_file tool that reads authorized files resolved relative to authorizer's sandbox. The authorizer must be non-nil.
func NewReadFileTool(authorizer authdomain.Authorizer) llmstream.Tool {
	sandboxAbsDir := authorizer.SandboxDir()
	return &toolReadFile{
		sandboxAbsDir: sandboxAbsDir,
		authorizer:    authorizer,
	}
}

// Name returns ToolNameReadFile, the registered name of the read_file tool.
func (t *toolReadFile) Name() string {
	return ToolNameReadFile
}

// Presenter returns the semantic presenter for read_file tool calls and results.
func (t *toolReadFile) Presenter() llmstream.Presenter {
	return readFilePresenterInstance
}

// Info returns the tool metadata for read_file, including its embedded description and JSON parameters. The returned schema requires path and accepts optional line_numbers
// and request_permission parameters.
func (t *toolReadFile) Info() llmstream.ToolInfo {
	return llmstream.ToolInfo{
		Name:        ToolNameReadFile,
		Description: strings.TrimSpace(descriptionReadFile),
		Parameters: map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "The path of the file to read (absolute, or relative to **sandbox** dir)",
			},
			"line_numbers": map[string]any{
				"type":        "boolean",
				"description": "Prefix each line with a line number if true",
			},
			"request_permission": map[string]any{
				"type":        "boolean",
				"description": "Optionally request the user's permission to run this command. Set to true for material access outside sandbox dir",
			},
		},
		Required: []string{"path"},
	}
}

// Run executes a read_file tool call and returns the requested file as a tagged text payload. It parses ParamsReadFile from call.Input, requires an existing file
// path, and authorizes the read when configured. Successful output is a `<file>` block containing valid UTF-8 content, optional 1-based line numbers, and metadata
// for the displayed name, processed line count, byte count, and truncation state. Parameter, path, authorization, and I/O failures are returned as error tool results.
func (t *toolReadFile) Run(ctx context.Context, call llmstream.ToolCall) llmstream.ToolResult {
	var params ParamsReadFile
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return NewToolErrorResult(call, fmt.Sprintf("error parsing parameters: %s", err), err)
	}

	if strings.TrimSpace(params.Path) == "" {
		return llmstream.NewErrorToolResult("path is required", call)
	}

	absPath, relPath, normErr := NormalizePath(params.Path, t.sandboxAbsDir, WantPathTypeFile, true)
	if normErr != nil {
		return NewToolErrorResult(call, normErr.Error(), normErr)
	}

	if t.authorizer != nil {
		if authErr := t.authorizer.IsAuthorizedForRead(params.RequestPermission, "", ToolNameReadFile, absPath); authErr != nil {
			return NewToolErrorResult(call, authErr.Error(), authErr)
		}
	}

	relToSandbox := relPath
	if relToSandbox == "" {
		relToSandbox = absPath
	}

	f, openErr := os.Open(absPath)
	if openErr != nil {
		return NewToolErrorResult(call, openErr.Error(), openErr)
	}
	defer f.Close()

	fi, statErr := f.Stat()
	if statErr != nil {
		return NewToolErrorResult(call, statErr.Error(), statErr)
	}

	// Read up to maxReadFileBytes into memory
	lr := &io.LimitedReader{R: f, N: maxReadFileBytes}
	raw, readErr := io.ReadAll(lr)
	if readErr != nil {
		return NewToolErrorResult(call, readErr.Error(), readErr)
	}

	fileTruncated := fi.Size() > int64(len(raw))

	// Filter out invalid UTF-8 bytes anywhere in the buffer while preserving valid content order
	// This preserves valid tails (e.g., "A\n") if the file starts with invalid bytes.
	buf := make([]byte, 0, len(raw))
	for i := 0; i < len(raw); {
		r, size := utf8.DecodeRune(raw[i:])
		if r == utf8.RuneError && size == 1 { // invalid byte
			fileTruncated = true
			i += 1
			continue
		}
		buf = append(buf, raw[i:i+size]...)
		i += size
	}

	// Prepare to process lines with limits
	content := string(buf)
	var out strings.Builder
	anyLineTruncated := false
	lineCount := 0
	outputBytes := 0
	processedLines := make([]string, 0, 1024)

	// We'll assemble final output with attributes after computing counts

	// Process lines up to maxReadFileLines
	reader := bufio.NewReader(strings.NewReader(content))
	for {
		if lineCount >= maxReadFileLines {
			fileTruncated = true
			break
		}
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			lineCount++
			// Truncate long lines by runes
			truncated := truncateRunes(line, maxLineLengthChars)
			if truncated != line {
				anyLineTruncated = true
			}
			processedLines = append(processedLines, truncated)
			outputBytes += len([]byte(truncated))
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return NewToolErrorResult(call, err.Error(), err)
		}
	}

	// If line numbers requested, prefix with left-padded numbers based on max width
	if params.LineNumbers && lineCount > 0 {
		width := len(strconv.Itoa(lineCount))
		for i := 0; i < lineCount; i++ {
			// 1-based numbering
			num := i + 1
			fmtNum := fmt.Sprintf("%*d:", width, num)
			out.WriteString(fmtNum)
			out.WriteString(processedLines[i])
		}
	} else {
		for i := 0; i < len(processedLines); i++ {
			out.WriteString(processedLines[i])
		}
	}

	// Ensure closing tag is on its own line without exceeding the 250KB inner limit when truncated by size.
	outStr := out.String()
	needsTrailingNewline := !strings.HasSuffix(outStr, "\n")
	if needsTrailingNewline {
		// If adding a newline would exceed the 250KB cap and the file was truncated by size,
		// drop the last rune from the output to make space for the newline.
		if fileTruncated && len([]byte(outStr))+1 > int(maxReadFileBytes) {
			if len(outStr) > 0 {
				rs := []rune(outStr)
				if len(rs) > 0 {
					outStr = string(rs[:len(rs)-1])
				}
			}
		}
	}

	// Build final output with tags and attributes
	var final strings.Builder
	final.WriteString("<file name=\"")
	final.WriteString(relToSandbox)
	final.WriteString("\" line-count=\"")
	final.WriteString(fmt.Sprintf("%d", lineCount))
	final.WriteString("\" byte-count=\"")
	final.WriteString(fmt.Sprintf("%d", outputBytes))
	final.WriteString("\" any-line-truncated=\"")
	if anyLineTruncated {
		final.WriteString("true")
	} else {
		final.WriteString("false")
	}
	final.WriteString("\" file-truncated=\"")
	if fileTruncated {
		final.WriteString("true")
	} else {
		final.WriteString("false")
	}
	final.WriteString("\">\n")
	final.WriteString(outStr)
	// Ensure closing tag is on its own line
	if needsTrailingNewline {
		final.WriteString("\n")
	}

	// Recompute byte-count from the actual inner content that will be emitted
	outputBytes = len([]byte(outStr))
	if needsTrailingNewline {
		outputBytes++
	}
	final.WriteString("</file>\n")

	return llmstream.ToolResult{CallID: call.CallID, Name: call.Name, Type: call.Type, Result: final.String()}
}

// The truncateRunes function returns the first maxRunes runes of s without splitting UTF-8 sequences. It returns an empty string when maxRunes is zero or negative.
func truncateRunes(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	// Fast path if already short enough
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	// Truncate by rune to avoid breaking UTF-8
	out := make([]rune, 0, maxRunes)
	count := 0
	for _, r := range s {
		if count >= maxRunes {
			break
		}
		out = append(out, r)
		count++
	}
	return string(out)
}

var readFilePresenterInstance llmstream.Presenter = readFilePresenter{}

// A readFilePresenter presents read_file tool calls as replacement summaries in the form "Read <path>".
type readFilePresenter struct{}

// Present returns a replacement presentation with the summary "Read <path>" for a read_file tool call. It uses the requested path when available and falls back
// to the call or tool name; result is ignored.
func (p readFilePresenter) Present(call llmstream.ToolCall, result *llmstream.ToolResult) llmstream.Presentation {
	_ = result

	return llmstream.Presentation{
		Behavior: llmstream.CompletionBehaviorReplace,
		Summary: llmstream.Line{
			JoinWithSpace: true,
			Segments: []llmstream.Segment{
				{Text: "Read", Role: llmstream.RoleAction},
				{Text: readFilePresenterTarget(call), Role: llmstream.RoleNormal},
			},
		},
	}
}

func readFilePresenterTarget(call llmstream.ToolCall) string {
	var params ParamsReadFile
	if err := json.Unmarshal([]byte(call.Input), &params); err == nil {
		if path := strings.TrimSpace(params.Path); path != "" {
			return path
		}
	}

	name := strings.TrimSpace(call.Name)
	if name == "" {
		name = ToolNameReadFile
	}
	return name
}
