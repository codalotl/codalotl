package updatedocs

import (
	"fmt"
	"testing"
)

func TestReflowDocComment(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		indent        int
		commentIndent int
		maxLineLength int
		want          string
	}{
		{
			name: "basic",
			input: dedent(`
				// Foo
				// bar baz
			`),
			indent:        0,
			commentIndent: 8,
			maxLineLength: 20,
			want: dedent(`
				// Foo bar baz
			`),
		},
		{
			name: "real example of short lines",
			input: dedent(`
				// Send sends the conversation to the model in order to get a RoleAssistant response message.
				// The last message in Messages MUST be a UserMessage.
				// If the request errors out, an error is returned. Additionally, the last UserMessage will contain details in a ResponseError struct.
			`),
			indent:        0,
			commentIndent: 4,
			maxLineLength: 180,
			want: dedent(`
				// Send sends the conversation to the model in order to get a RoleAssistant response message. The last message in Messages MUST be a UserMessage. If the request errors out, an error
				// is returned. Additionally, the last UserMessage will contain details in a ResponseError struct.
			`),
		},
		{
			name: "real example of short lines, many paragraphs",
			input: dedent(`
				// EnsureDocs adds missing documentation to pkg until all symbols that
				// qualify under includePrivate are documented.
				//
				// When includePrivate is false, only exported declarations are
				// considered; when true, unexported declarations and test helpers are
				// included as well.
				//
				// The returned slice lists files that were modified. If no change was
				// necessary, the slice is empty.
			`),
			indent:        0,
			commentIndent: 4,
			maxLineLength: 180,
			want: dedent(`
				// EnsureDocs adds missing documentation to pkg until all symbols that qualify under includePrivate are documented.
				//
				// When includePrivate is false, only exported declarations are considered; when true, unexported declarations and test helpers are included as well.
				//
				// The returned slice lists files that were modified. If no change was necessary, the slice is empty.
			`),
		},
		{
			name: "paragraphs",
			input: dedent(`
				// Hello world
				//
				// Another line here
			`),
			indent:        0,
			commentIndent: 8,
			maxLineLength: 20,
			want: dedent(`
				// Hello world
				//
				// Another line here
			`),
		},
		{
			name: "indent",
			input: dedent(`
				// Foo bar baz
			`),
			indent:        1,
			commentIndent: 8,
			maxLineLength: 40,
			want:          "\t// Foo bar baz\n",
		},
		{
			name: "bullet_improper",
			input: dedent(`
				// My List:
				// - item one is very long and should wrap around
				// * short
			`),
			indent:        0,
			commentIndent: 8,
			maxLineLength: 30,
			want: dedent(`
				// My List:
				//   - item one is very long and
				//     should wrap around
				//   - short
			`),
		},
		{
			name: "bullet_proper",
			input: dedent(`
				// My List:
				//   - item one is very long and
				//     should wrap around
				//   - short
			`),
			indent:        0,
			commentIndent: 8,
			maxLineLength: 50,
			want: dedent(`
				// My List:
				//   - item one is very long and should wrap around
				//   - short
			`),
		},
		{
			name: "very_long_single_line",
			input: dedent(`
				// This is a very long comment line that definitely exceeds the maximum line length and should be wrapped properly across multiple lines
			`),
			indent:        0,
			commentIndent: 8,
			maxLineLength: 30,
			want: dedent(`
				// This is a very long comment
				// line that definitely exceeds
				// the maximum line length and
				// should be wrapped properly across
				// multiple lines
			`),
		},
		{
			name:          "empty_input",
			input:         "",
			indent:        0,
			commentIndent: 8,
			maxLineLength: 80,
			want:          "",
		},
		{
			name: "only_empty_comments",
			input: dedent(`
				//
				//
				//
			`),
			indent:        0,
			commentIndent: 8,
			maxLineLength: 80,
			want:          "", // gofmt correctly deletes the doc
		},
		{
			name: "single_word",
			input: dedent(`
				// Hello
			`),
			indent:        0,
			commentIndent: 8,
			maxLineLength: 80,
			want: dedent(`
				// Hello
			`),
		},
		{
			name: "multiple_paragraphs_complex",
			input: dedent(`
				// Package foo provides utilities for data processing.
				//
				// This package includes several key components:
				// - Data validation
				// - Transformation pipelines
				//
				// Usage example:
				//	data := foo.New()
				//	data.Process()
			`),
			indent:        0,
			commentIndent: 8,
			maxLineLength: 60,
			want: dedent(`
				// Package foo provides utilities for data processing.
				//
				// This package includes several key components:
				//   - Data validation
				//   - Transformation pipelines
				//
				// Usage example:
				//
				//	data := foo.New()
				//	data.Process()
			`),
		},
		{
			name: "numbered_list_improper",
			input: dedent(`
				// Steps to follow:
				// 1. Initialize the system with proper configuration
				// 2. Load data from the source
				// 3. Process and validate
			`),
			indent:        0,
			commentIndent: 8,
			maxLineLength: 40,
			want: dedent(`
				// Steps to follow:
				//  1. Initialize the system with proper
				//     configuration
				//  2. Load data from the source
				//  3. Process and validate
			`),
		},
		{
			name: "numbered_list_proper_gofmt",
			input: dedent(`
				// Steps to follow:
				//  1. Initialize the system with proper configuration
				//  2. Load data from the source
				//  3. Process and validate
			`),
			indent:        0,
			commentIndent: 8,
			maxLineLength: 70,
			want: dedent(`
				// Steps to follow:
				//  1. Initialize the system with proper configuration
				//  2. Load data from the source
				//  3. Process and validate
			`),
		},
		{
			name: "numbered_list_with_long_items",
			input: dedent(`
				// Installation instructions:
				// 1. Download the package from the official repository and extract it to your desired location
				// 2. Configure environment variables including PATH and GOPATH
				// 3. Run the installation script with administrator privileges
			`),
			indent:        0,
			commentIndent: 8,
			maxLineLength: 60,
			want: dedent(`
				// Installation instructions:
				//  1. Download the package from the official repository and
				//     extract it to your desired location
				//  2. Configure environment variables including PATH and GOPATH
				//  3. Run the installation script with administrator privileges
			`),
		},
		{
			name: "numbered_list_with_indent",
			input: dedent(`
				// Steps:
				// 1. First step
				// 2. Second step
			`),
			indent:        2,
			commentIndent: 8,
			maxLineLength: 40,
			want:          "\t\t// Steps:\n\t\t//  1. First step\n\t\t//  2. Second step\n",
		},
		{
			name: "numbered_list_mixed_with_bullets",
			input: dedent(`
				// Process:
				// 1. Prepare data
				// 2. Process items
				//
				// Features:
				// - Fast processing
				// - Error handling
			`),
			indent:        0,
			commentIndent: 8,
			maxLineLength: 50,
			want: dedent(`
				// Process:
				//  1. Prepare data
				//  2. Process items
				//
				// Features:
				//   - Fast processing
				//   - Error handling
			`),
		},
		{
			name: "numbered_list_double_digit",
			input: dedent(`
				// Many steps:
				// 1. Step one
				// 2. Step two
				// 3. Step three
				// 4. Step four
				// 5. Step five
				// 6. Step six
				// 7. Step seven
				// 8. Step eight
				// 9. Step nine
				// 10. Step ten
				// 11. Step eleven
			`),
			indent:        0,
			commentIndent: 8,
			maxLineLength: 50,
			want: dedent(`
				// Many steps:
				//  1. Step one
				//  2. Step two
				//  3. Step three
				//  4. Step four
				//  5. Step five
				//  6. Step six
				//  7. Step seven
				//  8. Step eight
				//  9. Step nine
				//  10. Step ten
				//  11. Step eleven
			`),
		},
		{
			name: "mixed_bullet_types",
			input: dedent(`
				// Features:
				// - Authentication
				// * Authorization
				// • Data processing
				// + Error handling
			`),
			indent:        0,
			commentIndent: 8,
			maxLineLength: 50,
			want: dedent(`
				// Features:
				//   - Authentication
				//   - Authorization
				//   - Data processing
				//   - Error handling
			`),
		},
		{
			name: "code_blocks_preserved",
			input: dedent(`
				// Example usage:
				//   func main() {
				//     x := 42
				//     fmt.Println(x)
				//   }
			`),
			indent:        0,
			commentIndent: 8,
			maxLineLength: 30,
			want: dedent(`
				// Example usage:
				//
				//	func main() {
				//	  x := 42
				//	  fmt.Println(x)
				//	}
			`),
		},
		{
			name: "very_short_max_length",
			input: dedent(`
				// Hello world
			`),
			indent:        0,
			commentIndent: 8,
			maxLineLength: 15,
			want: dedent(`
				// Hello world
			`),
		},
		{
			name: "deep_indentation",
			input: dedent(`
				// This is a comment that needs deep indentation
			`),
			indent:        3,
			commentIndent: 8,
			maxLineLength: 40,
			want:          "\t\t\t// This is a comment\n\t\t\t// that needs deep\n\t\t\t// indentation\n",
		},
		{
			// NOTE: gofmt does not support nested bullets
			name: "nested_bullets",
			input: dedent(`
				// Main features:
				//   - Core functionality
				//     - Data processing
				//     - Error handling
				//   - Advanced features
				//     - Caching
				//     - Monitoring
			`),
			indent:        0,
			commentIndent: 8,
			maxLineLength: 50,
			want: dedent(`
				// Main features:
				//   - Core functionality
				//   - Data processing
				//   - Error handling
				//   - Advanced features
				//   - Caching
				//   - Monitoring
			`),
		},
		{
			name: "urls_and_long_identifiers",
			input: dedent(`
				// See https://example.com/very/long/path/to/documentation for details.
				// The SomeVeryLongStructNameThatShouldNotBeWrapped handles this.
			`),
			indent:        0,
			commentIndent: 8,
			maxLineLength: 40,
			want: dedent(`
				// See https://example.com/very/long/path/to/documentation
				// for details. The SomeVeryLongStructNameThatShouldNotBeWrapped
				// handles this.
			`),
		},
		{
			name: "preserve_special_formatting",
			input: dedent(`
				// ASCII art:
				//   +---+
				//   | X |
				//   +---+
			`),
			indent:        0,
			commentIndent: 8,
			maxLineLength: 30,
			want: dedent(`
				// ASCII art:
				//
				//	+---+
				//	| X |
				//	+---+
			`),
		},
		{
			name: "bullets_with_long_continuation",
			input: dedent(`
				// Requirements:
				// - The system must handle thousands of concurrent users while maintaining sub-second response times
				// - Data integrity must be preserved across all operations
			`),
			indent:        0,
			commentIndent: 8,
			maxLineLength: 50,
			want: dedent(`
				// Requirements:
				//   - The system must handle thousands of concurrent
				//     users while maintaining sub-second response times
				//   - Data integrity must be preserved across all operations
			`),
		},
		{
			name: "mixed_content_complex",
			input: dedent(`
				// Package validator provides comprehensive data validation.
				//
				// Key features include:
				// - Type checking for all primitive types
				// - Custom validation rules support
				// - Detailed error reporting with field-level precision
				//
				// Basic usage:
				//   v := validator.New()
				//   err := v.Validate(data)
				//   if err != nil {
				//     log.Fatal(err)
				//   }
				//
				// For advanced usage see the examples directory.
			`),
			indent:        0,
			commentIndent: 8,
			maxLineLength: 60,
			want: dedent(`
				// Package validator provides comprehensive data validation.
				//
				// Key features include:
				//   - Type checking for all primitive types
				//   - Custom validation rules support
				//   - Detailed error reporting with field-level precision
				//
				// Basic usage:
				//
				//	v := validator.New()
				//	err := v.Validate(data)
				//	if err != nil {
				//	  log.Fatal(err)
				//	}
				//
				// For advanced usage see the examples directory.
			`),
		},
		{
			name:          "trailing_spaces_and_tabs",
			input:         "// Hello world   \n// with spaces\t\n",
			indent:        0,
			commentIndent: 8,
			maxLineLength: 80,
			want: dedent(`
				// Hello world with spaces
			`),
		},
		{
			name: "large_comment_indent",
			input: dedent(`
				// Short line
				// This is a longer line that might wrap
			`),
			indent:        0,
			commentIndent: 20,
			maxLineLength: 40,
			want: dedent(`
				// Short line This is a longer line that
				// might wrap
			`),
		},
		{
			name: "preserve_code_indentation",
			input: dedent(`
				// Example:
				//     if err != nil {
				//         return fmt.Errorf("failed: %w", err)
				//     }
			`),
			indent:        0,
			commentIndent: 8,
			maxLineLength: 30,
			want: dedent(`
				// Example:
				//
				//	if err != nil {
				//	    return fmt.Errorf("failed: %w", err)
				//	}
			`),
		},
		{
			name: "numeric list with trailing paragarph",
			input: dedent(`
				// Example:
				// 1. Thing 1
				// 2. Thing 1
				// In conclusion, things are great.
			`),
			indent:        0,
			commentIndent: 8,
			maxLineLength: 80,
			want: dedent(`
				// Example:
				//  1. Thing 1
				//  2. Thing 1
				//
				// In conclusion, things are great.
			`),
		},
		{
			name: "list with trailing paragarph",
			input: dedent(`
				// Example:
				// - Thing 1
				// - Thing 1
				// In conclusion, things are great.
			`),
			indent:        0,
			commentIndent: 8,
			maxLineLength: 80,
			want: dedent(`
				// Example:
				//   - Thing 1
				//   - Thing 1
				//
				// In conclusion, things are great.
			`),
		},
		{
			// NOTE: user might think code is a sub paragraph.
			// They might expect the paragarph to wrap at 30. But because it's code, it doesn't wrap.
			// This is by design.
			name: "bullet_with_code",
			input: dedent(`
				// Features:
				// - Authentication system
				//
				//   Supports multiple providers including OAuth2, SAML, and custom.
				//
				// - Data processing
				//
				//   High-performance pipeline with concurrent processing.
			`),
			indent:        0,
			commentIndent: 8,
			maxLineLength: 30,
			want: dedent(`
				// Features:
				//   - Authentication system
				//
				//	Supports multiple providers including OAuth2, SAML, and custom.
				//
				//   - Data processing
				//
				//	High-performance pipeline with concurrent processing.
			`),
		},
		{
			name:          "inline_code_not_broken",
			input:         "// Use the function like `fmt.Printf(\"hello %s\", name)` to print formatted output.",
			indent:        0,
			commentIndent: 8,
			maxLineLength: 40,
			want:          "// Use the function like `fmt.Printf(\"hello %s\", name)`\n// to print formatted output.\n",
		},
		{
			name:          "inline_code_wrapped_in_parens",
			input:         "// Use printf (`fmt.Printf()`) to print text.",
			indent:        0,
			commentIndent: 8,
			maxLineLength: 80,
			want:          "// Use printf (`fmt.Printf()`) to print text.\n",
		},
		{
			name:          "triple_backtick_ok",
			input:         "// Wrap code blocks with ```.",
			indent:        0,
			commentIndent: 8,
			maxLineLength: 80,
			want:          "// Wrap code blocks with ```.\n",
		},
		{
			// NOTE: this is probably a behavior we want. Not explicitly implemented, but happens to work.
			name:          "wrapping_in_triple_backticks_works",
			input:         "// Use the function like ```fmt.Printf(\"hello %s\", name)``` to print formatted output.",
			indent:        0,
			commentIndent: 8,
			maxLineLength: 40,
			want:          "// Use the function like ```fmt.Printf(\"hello %s\", name)```\n// to print formatted output.\n",
		},
		{
			name:          "inline_code_doesnt_collapse_whitespace",
			input:         "// Use a comment like ` //   - ` to use bulleted lists.",
			indent:        0,
			commentIndent: 8,
			maxLineLength: 80,
			want:          "// Use a comment like ` //   - ` to use bulleted lists.\n",
		},
		{
			name:          "stray_backtick",
			input:         "// Use   a comment like `   x.",
			indent:        0,
			commentIndent: 8,
			maxLineLength: 80,
			want:          "// Use a comment like ` x.\n",
		},
		{
			name:          "inline_code_and_stray_backtick",
			input:         "// Use a comment like ` //   - ` to use bulleted lists. Love those  ` ",
			indent:        0,
			commentIndent: 8,
			maxLineLength: 80,
			want:          "// Use a comment like ` //   - ` to use bulleted lists. Love those `\n",
		},
		{
			name:          "inline_code_multiple_snippets",
			input:         "// Call `log.Info(\"message\")` and then use `fmt.Sprintf(\"formatted %d\", value)` for output.",
			indent:        0,
			commentIndent: 8,
			maxLineLength: 50,
			want:          "// Call `log.Info(\"message\")` and then use\n// `fmt.Sprintf(\"formatted %d\", value)` for output.\n",
		},
		{
			name:          "inline_code_very_long",
			input:         "// Execute `database.QueryRow(\"SELECT id, name, email FROM users WHERE active = ? AND created_at > ?\", true, time.Now())` for the query.",
			indent:        0,
			commentIndent: 8,
			maxLineLength: 50,
			want:          "// Execute `database.QueryRow(\"SELECT id, name, email FROM users WHERE active = ? AND created_at > ?\", true, time.Now())`\n// for the query.\n",
		},
		{
			name:          "inline_code_with_regular_text",
			input:         "// This function accepts a parameter called userID and then calls `api.GetUser(userID)` to retrieve user data from the remote service.",
			indent:        0,
			commentIndent: 8,
			maxLineLength: 60,
			want:          "// This function accepts a parameter called userID and then calls\n// `api.GetUser(userID)` to retrieve user data from the remote\n// service.\n",
		},
		{
			name:          "combined_indent_and_comment_indent_basic",
			input:         "// This is a long comment line that should wrap properly considering both tab indentation and comment spacing.",
			indent:        2,
			commentIndent: 8,
			maxLineLength: 50,
			want:          "\t\t// This is a long comment line that\n\t\t// should wrap properly considering\n\t\t// both tab indentation and comment\n\t\t// spacing.\n",
		},
		{
			name: "doesn't clobber go:embed",
			input: dedent(`
				//go:embed some/path.txt
			`),
			indent:        0,
			commentIndent: 8,
			maxLineLength: 80,
			want: dedent(`
				//go:embed some/path.txt
			`),
		},
		{
			name: "go:build directive in combination with other comments",
			input: dedent(`
				//Embed file into var:
				//go:embed some/path.txt
			`),
			indent:        0,
			commentIndent: 8,
			maxLineLength: 80,
			want: dedent(`
				// Embed file into var:
				//
				//go:embed some/path.txt
			`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reflowDocComment(tt.input, tt.indent, tt.commentIndent, tt.maxLineLength)
			if got != tt.want {

				fmt.Println("Got formatted nicely:")
				fmt.Println(got)

				t.Fatalf("got:\n%q\nwant:\n%q", got, tt.want)
			}
		})
	}
}
