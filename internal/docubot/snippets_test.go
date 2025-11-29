package docubot

import "testing"

func TestExtractSnippets(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "empty input",
			input:    "",
			expected: []string{},
		},
		{
			name:     "no code blocks",
			input:    "This is a regular text\nwith no code blocks",
			expected: []string{},
		},
		{
			name:  "single code block",
			input: "Here's some text\n```go\nfunc main() {\n    fmt.Println(\"hello\")\n}\n```\nMore text",
			expected: []string{
				"func main() {\n    fmt.Println(\"hello\")\n}\n",
			},
		},
		{
			name:  "multiple code blocks",
			input: "First block:\n```python\nprint(\"hello\")\n```\nSecond block:\n```go\nfmt.Println(\"world\")\n```\nMore text",
			expected: []string{
				"print(\"hello\")\n",
				"fmt.Println(\"world\")\n",
			},
		},
		{
			name:  "code block with language specifier",
			input: "```go\nfunc test() {\n    return true\n}\n```\nMore text",
			expected: []string{
				"func test() {\n    return true\n}\n",
			},
		},
		{
			name:     "unclosed code block",
			input:    "Here's some text\n```go\nfunc main() {\n    fmt.Println(\"hello\")\n}\nMore text without closing backticks",
			expected: []string{}, //  NOTE: it's arguable whether this is an error, expect no snippet, or expect snippet to be the code until EOF. This answer is easy for now
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractSnippets(tt.input)
			if len(got) != len(tt.expected) {
				t.Errorf("extractSnippets() got %d snippets, want %d", len(got), len(tt.expected))
				return
			}
			for i, snippet := range got {
				if snippet != tt.expected[i] {
					t.Errorf("snippet %d = %q, want %q", i, snippet, tt.expected[i])
				}
			}
		})
	}
}

func TestUnwrapSingleSnippet(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no fences returns unchanged",
			input:    "some text\nwith lines",
			expected: "some text\nwith lines",
		},
		{
			name:     "fenced with language, closing on own line",
			input:    "```go\nfmt.Println(\"hi\")\n```",
			expected: "fmt.Println(\"hi\")\n",
		},
		{
			name:     "fenced with language, closing on own line",
			input:    "\n```go\nfmt.Println(\"hi\")\n```\n",
			expected: "fmt.Println(\"hi\")\n",
		},
		{
			name:     "fenced without language, closing on own line",
			input:    "```\nline1\nline2\n```",
			expected: "line1\nline2\n",
		},
		{
			name:     "fenced with language, closing immediately at end returns unchanged (inline close ignored)",
			input:    "```python\nprint('x')```",
			expected: "```python\nprint('x')```",
		},
		{
			name:     "starts with fence but missing closing returns unchanged",
			input:    "```js\nconsole.log('no close')\n",
			expected: "```js\nconsole.log('no close')\n",
		},
		{
			name:     "starts with fence but extra text after closing returns last snippet",
			input:    "```\ncode\n```\ntrailing",
			expected: "code\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := unwrapSingleSnippet(tt.input)
			if got != tt.expected {
				t.Fatalf("unexpected result.\ninput:\n%q\nexpected:\n%q\ngot:\n%q", tt.input, tt.expected, got)
			}
		})
	}
}
