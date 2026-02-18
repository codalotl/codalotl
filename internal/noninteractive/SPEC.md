# noninteractive

The `noninteractive` package implements a noninteractive agent. It's the analogue of `tui`, except non-interactive and via standard CLI prints, instead of an alt screen.

## Details

### Tool Calls

The agent issues events for tool calls and tool call results. Usually running the tool is fast, so the Call -> Result takes just milliseconds (for instance, reading a file).

To avoid printing "duplicate messages" serially (ex: `• Read foo/bar.go`, first with a grey bullet, then a green bullet), we do the following:
- Upon getting a tool call, start a 3 second timer.
- If we get the corresponding result within 3 seconds, only print the result and cancel the timer.
- If the three seconds elapses without getting the result, print the tool call. When the result comes in, print that as well.

### Finishing a session

Upon finishing a session, print a line like this:

`• Agent finished the turn. Tokens: input=10042 cached_input=32000 output=1043 total=43085`

## Public API

```go
// IsPrinted returns true if err has already been printed to the screen.
func IsPrinted(err error) bool

type Options struct {
	// working directory / sandbox dir. If "", uses os.Getwd()
	CWD string

	// PackagePath sets package mode with the path to a package vs CWD. If "", does not use package mode. PackagePath can be any filesystem path (ex: "."; "/foo/bar";
	// "foo/bar"; "./foo/bar"). It must be rooted inside of CWD.
	PackagePath string

	ModelID     llmmodel.ModelID // ModelID selects the LLM model for this run. If empty, uses the existing default model behavior.
	LintSteps   []lints.Step     // LintSteps controls which lint steps the agent runs.
	ReflowWidth int              // ReflowWidth is the width for reflowing documentation with the `updatedocs` package.
	AutoYes     bool             // Answers 'Yes' to any permission check. If false, we answer 'No' to any permission check. The end-user is never asked.

	// NoFormatting=true means any prints do NOT use colors or other ANSI control codes to format. Only outputs plain text. Otherwise, we default to the color scheme
	// of the terminal and print colorized/formatted text.
	NoFormatting bool

	// If Out != nil, any prints we do will use Out; otherwise will use Stdout. If Exec encounters errors during its run (eg: cannot talk to LLM; cannot write file),
	// we'd still just print to Out (instead of something like Stderr).
	Out io.Writer
}

// Exec runs the agent with prompt and opts. It prints messages, tool calls, and so on to the screen.
//
// If there's any validation error (anything before the agent actually starts), an error is returned and nothing is nothing is printed. If there's an unhandled error
// and the agent cannot complete its run (ex: cannot talk to LLM, even after retries), a message may be printed AND returned via err. Callers can use IsPrinted to
// determine if an error has already been printed. Finally, note that many "errors" happen in the course of typical agent runs. For instance, the agent will ask
// to read non-existant files; shell commands will fail; etc. These do not typically constitute errors worthy of being returned (instead, the LLM is just told a
// file doesn't exist).
func Exec(userPrompt string, opts Options) error
```