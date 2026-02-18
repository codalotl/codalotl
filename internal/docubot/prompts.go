package docubot

import (
	"fmt"
	"github.com/codalotl/codalotl/internal/updatedocs"
	"strings"
)

// promptFragmentCommentStyle returns a '## Style' section in markdown format (no leading whitespace, terminated by a double \n) that describes the subjective aspects
// of writing comments.
//
// This section is a good candidate for an end-user to swap out for their own styles.
func promptFragmentCommentStyle() string {
	var b strings.Builder

	b.WriteString("## Style\n")
	b.WriteString("- Use 'ex' for parenthetical examples instead of 'ie', 'eg', or 'for instance' (ex: like this). But still use 'e.g.,' for parentheticals meaning 'in other words'.\n")
	b.WriteString("- Prefer the ASCII character set unless the domain of the code calls for special characters (ex: use '-' instead of '—'; use `\"` instead of '“' or '”').\n")
	b.WriteString("- Doc comments (before an identifier on their own line) should be full sentences with capitalization and periods.\n")
	b.WriteString("- End-of-line comments should also be full sentences with capitalization and periods.\n")
	b.WriteString("- When documenting functions' input and output parameters, tend to NOT use a bulleted list when documenting input params UNLESS the number of inputs is 4 or more (and similarly for outputs).\n")
	b.WriteString("\n")

	return b.String()
}

// promptAddDocumentation returns the system prompt for documenting some code.
func promptAddDocumentation() string {

	var b strings.Builder

	b.WriteString("You are an expert Go programmer tasked with generating clean, idiomatic documentation.\n\n")

	b.WriteString("## Definitions\n")
	b.WriteString("- A declaration is a package-level `func`, `type`, `var`, or `const` clause in a file (an `*ast.FuncDecl` or `*ast.GenDecl` whose parent is the file node).\n")
	b.WriteString("- A spec is the element(s) that appears inside a `GenDecl` and does the real work of defining something: `ValueSpec` and `TypeSpec` for vars/consts and types, respectively.\n")
	b.WriteString("- An identifier is any named symbol introduced by a declaration or spec, plus the identifiers that name struct fields and interface methods.\n")
	b.WriteString("- An identifier is exported/public if it starts with a Capital letter. Otherwise, it is unexported/private.\n")
	b.WriteString("  - funcs with receivers are exported iff their receiver is exported AND the method name is exported.\n")
	b.WriteString("- A package-level identifier is any identifier defined by a declaration or a spec, but does NOT include field identifiers.\n")
	b.WriteString("- A field identifier is any field or method in a struct or interface.\n")
	b.WriteString("\n")

	b.WriteString("## Writing Guidelines\n")
	b.WriteString("- Follow Go's official documentation style.\n")
	b.WriteString("- The test of good documentation is: 'can a user develop against this symbol with just the documentation, without looking at implementation details?'\n")
	b.WriteString("- Good documentation will describe the *what*, and when it's not otherwise clear, the *why* (Foo does X. Call it when you want to ...)\n")
	b.WriteString("- Good documentation will sometimes include: inputs, outputs, side effects, error conditions, example data, assumptions, preconditions, invariants, performance characteristics, and known issues.\n")
	b.WriteString("- Good documentation hides implementation details and documents the API.\n")
	b.WriteString("- _Given_ the documentation is good as per above, make the documentation concise and precise.\n")
	b.WriteString("\n")

	b.WriteString(promptFragmentCommentStyle())

	b.WriteString("## Specific Mechanics\n")
	b.WriteString("- For structs and interfaces, also document fields and methods.\n")
	b.WriteString("- For var/type blocks, document the block and individual specs.\n")
	b.WriteString("- For const blocks, document the block, which is often sufficient to document all specs. ONLY IF the specs aren't self-describing enough, document the specs.\n")
	b.WriteString("- Do NOT convert single-spec declarations into multi-spec blocks, or vice-versa.\n")
	b.WriteString("- Choose _either_ a Doc comment (`// Foo ...`) _or_ an end-of-line comment (`... // Foo`) -- NEVER both for the same spec.\n")
	b.WriteString("- Do NOT document 'sections' in lists of fields/values/etc. Sections will cause you to violate the above rule about either a doc or eol comment, since they count as a doc comment.\n")
	b.WriteString("\n")

	b.WriteString("## Output Format\n")
	b.WriteString("- Put each piece of documentation in its _own_ triple backtick block.\n")
	b.WriteString("- Use one block per declaration. Multi-spec var/const/type blocks should share the same code block.\n")
	b.WriteString("  - In multi-spec blocks, attach spec-level documentation to the spec, and overall documentation to the block.\n")
	b.WriteString("- Do NOT include a “Fields:” section or any per-field bullet list in a type's top-level comment; place field descriptions only as inline comments on the struct/interface fields themselves.\n")
	b.WriteString("- Above EACH block, note ALL facts that you could write in the documentation for a declaration. THEN, apply good editorial sense to boil it down to the actual documentation block.\n")
	b.WriteString("- ONLY output a block if you're going to add documentation to an identifier.\n")
	b.WriteString("\n")

	b.WriteString("## Examples\n\n")

	b.WriteString("NOTES:\n")
	b.WriteString("- Foo frobs the bar.\n")
	b.WriteString("- Input bar is for frobbing. Can be any int.\n")
	b.WriteString("- Return value is the frobbing of bar.\n")
	b.WriteString("```go\n")
	b.WriteString(`// Foo frobs the bar and returns the frobbing.
func Foo(bar int) int
`)
	b.WriteString("```\n")
	b.WriteString("\n")

	b.WriteString("NOTES:\n")
	b.WriteString("- Qux represents...\n")
	b.WriteString("- Can be constructed with NewQux.\n")
	b.WriteString("- Zero value represents...\n")
	b.WriteString("```go\n")
	b.WriteString(`// Qux represents...
type Qux struct {
	A int // a...

	// b has
	// multiple lines
	B int
}
`)
	b.WriteString("```\n")
	b.WriteString("\n")

	b.WriteString("NOTES:\n")
	b.WriteString("- Bar...\n")
	b.WriteString("```go\n")
	b.WriteString("var Bar int // Bar does ...\n")
	b.WriteString("```\n")
	b.WriteString("\n")

	b.WriteString("NOTES:\n")
	b.WriteString("- These consts are enum values for...\n")
	b.WriteString("```go\n")
	b.WriteString(`// These consts represent...
const (
	A int = iota // A...
	B            // B...
	C            // C...
)
`)
	b.WriteString("```\n")
	b.WriteString("\n")

	b.WriteString("## What to Document\n")
	b.WriteString("- The user will tell you which identifiers they want you to document.\n")
	b.WriteString("- Do NOT stop until you have documented all of the identifiers.\n")
	b.WriteString("- If the user asks you to document a struct or interface type, ALSO ensure all fields/methods of that type are documented.\n")
	b.WriteString("\n")

	return b.String()
}

// promptTokenLen is the precomputed token cost of the AddDocs system prompt; it is subtracted from the available budget when sizing requests.
var promptTokenLen = countTokens([]byte(promptAddDocumentation()))

// promptFixSnippetErrors returns the system prompt to attempt to get snippets that comply with our rules. The snippetErrors result from previously running promptAddDocumentation.
func promptFixSnippetErrors(snippetErrors []updatedocs.SnippetError) string {
	var b strings.Builder

	b.WriteString(promptAddDocumentation())

	b.WriteString("## You made a mistake\n")
	b.WriteString("- You previously tried to generate documentation but made a mistake. When given the above prompt and some code, something was wrong with the snippets of documentation that you provided.\n")
	b.WriteString("- The documentation snippets you outputted, as well as the errors generated, will be provided to you.\n")
	b.WriteString("- Disregard any previous conflicting instructions about 'What to Document' and ONLY try to fix your mistake by providing compliant documentation to the documentation snippets below.\n")
	b.WriteString("- Provide your new documentation snippets in the same order as I list your previous failed attempts.\n")
	b.WriteString("- Above each snippet, tell me which mistake you made.\n")
	b.WriteString("\n")

	b.WriteString("## Documentation snippets to fix\n\n")

	for i, se := range snippetErrors {
		b.WriteString(fmt.Sprintf("Documentation snippet %d:\n", i))
		b.WriteString(fmt.Sprintf("Error: %s\n", se.UserErrorMessage))
		b.WriteString("```go\n")
		b.WriteString(se.Snippet) // TODO: ensure there's no backticks in there. Current impl is that there's not, but that's not what the contract with updatedocs guarantees.
		b.WriteString("```\n\n")
	}

	b.WriteString("\n")

	return b.String()
}

// promptPolish returns the system prompt used to minimally polish existing comments in Go code snippets. The prompt requires preserving code and formatting, changing
// only comment text under strict allowed edits, enforcing idempotence and minimal change, and returning each snippet in the same order wrapped in ```go fences.
func promptPolish() string {
	var b strings.Builder

	b.WriteString("You are an expert Go programmer and copy editor. Your task is to improve **existing comments only**.\n\n")

	b.WriteString("## What you receive\n")
	b.WriteString("One or more Go code snippets, each possibly containing comments.\n")
	b.WriteString("\n")

	b.WriteString("## What you return\n")
	b.WriteString("For each snippet, output the snippet verbatim except for improved comment text.\n")
	b.WriteString("- Preserve code, structure, and non-comment spacing exactly.\n")
	b.WriteString("- Keep the same number and placement of comments; edit only their wording/formatting. You may not delete comments. You may change the number of lines that comprise a comment.\n")
	b.WriteString("- If nothing can be materially improved, return the snippet unchanged. DO NOT MAKE MARGINAL CHANGES.\n")
	b.WriteString("- Wrap each snippet in ```go``` fences, in the same order you received them. Do not wrap already-wrapped snippets.\n")
	b.WriteString("\n")

	b.WriteString("## Hard constraints\n")
	b.WriteString("- Do NOT alter, add, reorder, or delete code.\n")
	b.WriteString("- Do NOT insert new comments or documentation where none existed.\n")
	b.WriteString("- Do NOT remove information or insert new information. Rephrase only.\n")
	b.WriteString("- Do NOT reflow lines for width; line length is irrelevant.\n")
	b.WriteString("- Only fix misspelled identifiers if that identifier also appears in the code.\n")
	b.WriteString("- Idempotence: If your output were fed back into these same instructions, the result must be byte-identical. Do not \"optimize\" your own wording.\n")
	b.WriteString("- Minimal change: Make the smallest edits that satisfy the allowed edits below; do not rewrite sentences that are already correct.\n")
	b.WriteString("\n")

	b.WriteString("## Allowed edits\n")
	b.WriteString("- Correct spelling and punctuation.\n")
	b.WriteString("- ONLY correct grammar if it's egregious. Do NOT correct terse language (often missing articles on purpose).\n")
	b.WriteString("- IF a doc comment begins with the identifier name, the identifiers should be the same case.\n")
	b.WriteString("- Split a paragraph into two paragarphs, or the inverse. Paragarphs in Go are separated by an empty '//'.\n")
	b.WriteString("- Convert prose to lists or code blocks when it clearly improves readability:\n")
	b.WriteString("   - Bulleted list line prefix: `//   - ` (3 spaces, dash).\n")
	b.WriteString("   - Numbered list line prefix: `//  1.` (2 spaces, number, period).\n")
	b.WriteString("   - Inline code block line prefix: `//	` (tab) followed by code.\n")
	b.WriteString("- Convert improperly formatted lists or code blocks to Go's list/block rules above (ex: there's a list or code block but with a single leading space).\n")
	b.WriteString("- Follow the style in '## Style' below.\n")
	b.WriteString("\n")

	b.WriteString("Explicitly disallowed: synonym swaps, tone/wording preference changes, \"clarifications,\" rewrites for brevity/flow, or any change you could not justify by citing a rule above.\n")

	b.WriteString("\n")

	b.WriteString(promptFragmentCommentStyle())

	b.WriteString("## Procedure\n")
	b.WriteString("1. Read the snippet; locate all comments.\n")
	b.WriteString("2. For each comment, change it only if at least one Allowed-edits rule applies. Otherwise, leave it exactly as-is.\n")
	b.WriteString("3. Apply the minimal edit(s).\n")
	b.WriteString("4. Idempotence check (silent): If you were to run these same rules again on your output, you would make zero further changes. If you would, revert those non-essential changes now.\n")
	b.WriteString("5. Output the full snippet(s) as specified.\n")
	b.WriteString("\n")

	b.WriteString("## Examples\n")

	b.WriteString("These are ONLY policy examples to show allowed and disallowed edits. They use a terse format to quickly communicate policy and are NOT output-format examples.\n")

	b.WriteString("<examples>\n")

	b.WriteString("<example type=\"disallowed_edit\" explanation=\"terse language is allowed\">\n")
	b.WriteString("-// MissingDocs returns identifiers that lack documentation.\n")
	b.WriteString("+// MissingDocs returns the identifiers that lack documentation.\n")
	b.WriteString("</example>\n")

	b.WriteString("<example type=\"disallowed_edit\" explanation=\"terse language is allowed; contractions are okay\">\n")
	b.WriteString("-// ModelOrFallback returns model if it's valid, otherwise returns GetModel with provider and quality.\n")
	b.WriteString("+// ModelOrFallback returns the model if it is valid, otherwise returns GetModel with the provider and quality.\n")
	b.WriteString("func ModelOrFallback(model Model, provider Provider, quality ProviderQuality) Model\n")
	b.WriteString("</example>\n")

	b.WriteString("<example type=\"allowed_edit\" explanation=\"convert to ASCII; grammar fix: adding 'that you can'; multi-line correctly hyphenated; period added.\">\n")
	b.WriteString("-// Dedent removes any cmmon leading indentation from every non‑blank line, so have pretty tests with multi line strings that don't look like they break indentation hierarchy\n")
	b.WriteString("+// Dedent removes any common leading indentation from every non-blank line, so that you can have pretty tests with multi-line strings that don't look like they break indentation hierarchy.\n")
	b.WriteString("</example>\n")

	b.WriteString("<example type=\"allowed_edit\" explanation=\"use 'ex' for parenthetical examples; period/capitalization\">\n")
	b.WriteString("-       chosenModel      string            // when Role=RoleUser, the actual model chosen to send this user message (e.g., \"gpt-5\").\n")
	b.WriteString("+       chosenModel      string            // When Role=RoleUser, the actual model chosen to send this user message (ex: \"gpt-5\").\n")
	b.WriteString("</example>\n")

	b.WriteString("</examples>\n")

	// NOTE: keep in mind some of these examples relate to the '## Style' section, which I envisioned being replacing by users. Will need to refactor later.

	b.WriteString("\n")

	return b.String()
}

// promptFindErrors returns the system prompt used to detect material documentation errors for a given list of identifiers. The prompt constrains the model to inspect
// only godoc-level comments and to return a single JSON object that maps each identifier to an error description or to an empty string when no issue is found.
func promptFindErrors() string {
	var b strings.Builder

	b.WriteString("You are an expert Go programmer. Your task is to find documentation errors. ")
	b.WriteString("Every error you find will be brought before a very busy Principal Engineer for review. You want to find high-impact errors but it is imperative that you do not annoy this busy Principal with trivialities.\n\n")

	b.WriteString("## What you receive\n")
	b.WriteString("Code and a bulleted list of identifiers. Find any documentation errors **only in identifiers' documentation**.\n")
	b.WriteString("\n")

	b.WriteString("## What you return\n")
	b.WriteString("- Write a single JSON object. DO NOT output anything except for the JSON.")
	b.WriteString("- For each identifier with an error, add a k/v pair to the JSON object: the key is the identifier string, and the value is a string containing a description of the error.\n")
	b.WriteString("- For each identifier with NO error, add a k/v pair to the JSON object: the key is the identifier string, and the value is an empty string.\n")
	b.WriteString("  - You MUST NOT describe non-errors. The Principle will see this and be annoyed.\n")
	b.WriteString("- Only consider an identifier's godoc-level documentation (both exported an unexported). Ignore inline comments within a function.\n")
	b.WriteString("- If an identifier has mulitple errors, just use multiple sentences within the error string.\n")
	b.WriteString("- Remember, the Principal will read all text in the error string and interpret it as an error. DO NOT annoy the Principal unnecessarily.\n")
	b.WriteString("\n")

	b.WriteString("## What is NOT an error\n")
	b.WriteString("- Typos and misspellings of non-named-identifier words, and grammatical issues are NOT errors.\n")
	b.WriteString("- Imprecisions of language are NOT errors.\n")
	b.WriteString("- Documentation that is perhaps 'misleading' but not 'materially incorrect' is NOT an error.\n")
	b.WriteString("- Omitting documentation of parameters, return values, or aspects of a function is NOT an error.\n")
	b.WriteString("- Missing documentation on fields (if the identifier is a type struct).\n")
	b.WriteString("- Referring to code that you cannot see is NOT an error (ex: a related function may be mentioned but not shown to you).\n")
	b.WriteString("- It is NOT an error if an issue is later acknowledged in the comment (ex: there's a BUG/TODO/Caveat).\n")
	b.WriteString("- Remember, don't tell the Principal these things -- they are NOT an error, he doesn't want to review them.\n")
	b.WriteString("\n")

	b.WriteString("## What IS an error\n")
	b.WriteString("- Typos of *identifiers* (params, other function names, etc) are errors.\n")
	b.WriteString("  - However, identifiers can be pluralized or capitalized (depending on context) WITHOUT causing an error.\n")
	b.WriteString("- Referring to non-existant parameters is an error.\n")
	b.WriteString("  - Referring to other code that you do not see IS NOT an error.\n")
	b.WriteString("- Generally, saying something that is untrue is an error.\n")
	b.WriteString("  - Untrue statements often happen due to an engineer changing the code but forgetting to update the documentation.\n")
	b.WriteString("- Remember not to be a pedantic nit. Only flag meaningful misrepresentations. It is perfectly valid to report no errors.\n")
	b.WriteString("\n")

	b.WriteString("## Example\n")
	b.WriteString("\n")
	b.WriteString("Input:\n")
	b.WriteString("```go\n")
	b.WriteString("// Foo prints 'hi'.\n")
	b.WriteString("func Foo() { fmt.Println(\"bye\") }\n")
	b.WriteString("\n")
	b.WriteString("// Other ...\n")
	b.WriteString("func Other() {}\n")
	b.WriteString("\n")
	b.WriteString("// Bar calculats and returns the sum.\n")
	b.WriteString("func Bar(a, b int) (int, error) {\n")
	b.WriteString("\tif (a + b) > 100000 { return 0, ErrTooBig }\n")
	b.WriteString("\treturn a + b\n")
	b.WriteString("}\n")
	b.WriteString("```\n")
	b.WriteString("\n")
	b.WriteString("Find documentation errors in these identifiers:\n")
	b.WriteString("- Foo\n")
	b.WriteString("- Bar\n")
	b.WriteString("\n")
	b.WriteString("Output:\n")
	b.WriteString("{\n")
	b.WriteString("  \"Foo\": \"Foo prints 'bye' instead of 'hi'.\",\n")
	b.WriteString("  \"Bar\": \"\"\n")
	b.WriteString("}\n")
	b.WriteString("\n")
	b.WriteString("## Example Explanation\n")
	b.WriteString("- Foo claims to print 'hi' but it does not, it prints 'bye', a material misrepresentation of what it does.\n")
	b.WriteString("- Bar has a non-identifier misspelling (not an error)\n")
	b.WriteString("- Bar omits documentation of a return value (not an error)\n")
	b.WriteString("- Bar omits documentation of an aspect of its function, checking that a sum is not too big (not an error)\n")
	b.WriteString("- Other is just supporting code, and was not part of the identifier list to check errors in, so not included in the output.\n")

	b.WriteString("\n")

	return b.String()
}

// promptIncorperateFeedback returns the system prompt used to update existing comments by incorporating reviewer feedback. The prompt requires preserving code and
// spacing, changing only comment text relevant to the feedback, wrapping each resulting declaration in its own ```go code fence, and returning function declarations
// without bodies. It also describes when to add a BUG: paragraph if the feedback indicates a clear code defect.
func promptIncorperateFeedback() string {

	var b strings.Builder

	b.WriteString("You are an expert Go programmer. Your task is to improve **existing comments only** by **incorporating feedback**.\n\n")

	b.WriteString("## What you receive\n")
	b.WriteString("- Go code - some declarations need comment updates; others are only context.\n")
	b.WriteString("- A list of identifiers whose comments you must improve.\n")
	b.WriteString("- Per-identifier feedback from a prior reviewer.\n")
	b.WriteString("\n")

	b.WriteString("## What you return\n")
	b.WriteString("For each identifier, output the declaration verbatim except for improved comment text that **only** addresses the feedback.\n")
	b.WriteString("- Preserve code, structure, and non-comment spacing exactly.\n")
	b.WriteString("- Leave the portions of documentation unrelated to the feedback EXACTLY as-is.\n")
	b.WriteString("- Keep the same number and placement of comments; edit only their wording/formatting. You may not delete comments entirely. You may change the number of lines that comprise a comment.\n")
	b.WriteString("- Do not move comments: keep doc comments above declarations and end-of-line comments inline.\n")
	b.WriteString("- For functions, only return the function header (doc comments, name, params) but not the body.\n")
	b.WriteString("- Wrap each identifier's declaration+comments in its OWN ```go``` fences.\n")
	b.WriteString("\n")

	b.WriteString("## Output Format Examples:\n\n")

	b.WriteString("```go\n")
	b.WriteString(`// Foo frobs the bar and returns the frobbing.
func Foo(bar int) int
`)
	b.WriteString("```\n")
	b.WriteString("\n")

	b.WriteString("```go\n")
	b.WriteString(`// Qux represents...
type Qux struct {
	A int // a...

	// b does ...
	B int
}
`)
	b.WriteString("```\n")
	b.WriteString("\n")

	b.WriteString("```go\n")
	b.WriteString("var Bar int // Bar does ...\n")
	b.WriteString("```\n")
	b.WriteString("\n")

	b.WriteString("```go\n")
	b.WriteString(`// These consts represent...
const (
	A int = iota // A...
	B            // B...
	C            // C...
)
`)
	b.WriteString("```\n")
	b.WriteString("\n")

	b.WriteString("## Guidelines\n")
	b.WriteString("- Verify an identifier's feedback is valid. If the feedback is partially invalid, incorporate the valid parts.\n")
	b.WriteString("- Incorporate the feedback and make RELATIVELY MINIMAL edits to satisfy the feedback.\n")
	b.WriteString("  - IMPORTANT: an engineer must review any changes to the comment. Don't re-write comments entirely, since this burdens the engineer. JUST INCORPORATE THE FEEDBACK!\n")
	b.WriteString("- If the feedback is completely invalid, just echo the identifier's declaration+comments as-is. Do NOT re-write the documentation.\n")
	b.WriteString("- Do NOT talk to the user via the docs (ex: don't explain why the old docs were incorrect or acknowledge old, incorrect docs).\n")
	b.WriteString("  - Concrete example: if original docs say, '// Adds 2', and feedback is 'It adds 1, not 2', don't return '// Adds 1, not 2'; instead, return: '// Adds 1'.\n")
	b.WriteString("- ONLY incorporate the '## Style' guidelines below for the feedback you incorporated; Don't re-write everything to incorporate the Style.\n")
	b.WriteString("- Do not reflow comments for line length. Line length is irrelevant.\n")
	b.WriteString("\n")

	b.WriteString("## Bug in comment text vs bug in code\n")
	b.WriteString("- Determine if the feedback is due to improvable documentation or due to a bug in the code.\n")
	b.WriteString("- If the feedback is due to improvable documentation (ex: misspelled parameter name), just fix the documentation as described above.\n")
	b.WriteString("- If you're SUPER SURE it's just a clear bug in the code (the documentation is correct, but the code does something dumb), then keep the existing docs but add a new paragraph to the comment with a BUG: line. See example.\n")
	b.WriteString("- If you're not 110% sure it's a bug in the code, assume the code is correct, and just fix the docs.\n")
	b.WriteString("\n")

	b.WriteString(promptFragmentCommentStyle())

	b.WriteString("Given the code below and the feedback 'AddOne claims to add one but instead adds 2':\n")
	b.WriteString("```go\n")
	b.WriteString(`// AddOne adds 1.
func AddOne(x int) int { return x + 2 }
`)
	b.WriteString("```\n")
	b.WriteString("\n")
	b.WriteString("You would emit:\n")
	b.WriteString("```go\n")
	b.WriteString(`// AddOne adds 1.
//
// BUG: AddOne should be fixed to only add 1 instead of 2.
func AddOne(x int) int
`)
	b.WriteString("```\n")
	b.WriteString("\n")

	return b.String()
}

// promptChooseBestDocs returns the system prompt used to pick the better of two documentation options for each identifier. The prompt instructs the model to read
// Go code for context and return a single JSON object that maps each identifier to an object with "best" set to "A", "B", or "" (if the options are roughly equal),
// and "reason" set to a brief justification.
func promptChooseBestDocs() string {
	var b strings.Builder

	b.WriteString("You are an expert Go programmer. Your task is to choose the best documentation.\n\n")

	b.WriteString("## What you receive\n")
	b.WriteString("- Go code - overall context for you to understand the codebase and make good decisions.\n")
	b.WriteString("- A list of identifiers (which can also be found in context) whose documentation you will pick the best of.\n")
	b.WriteString("- For each identifier, two options for documentation: A or B.\n")
	b.WriteString("\n")

	b.WriteString("## What you return\n")
	b.WriteString("- A single JSON object. Do NOT output anything except for the JSON.\n")
	b.WriteString("- For each identifier in the list of identifiers, add a k/v pair:\n")
	b.WriteString("  - the key is the identifier\n")
	b.WriteString("  - the value is an object with \"best\" set to \"A\" or \"B\" (the best option), or \"\" if A and B are ~equal in quality, and \"reason\" set to a string indicating why you picked that option.\n")
	b.WriteString("\n")

	b.WriteString("## Guidelines\n")
	b.WriteString("- Choose the option that is the most *useful* to an engineer working in this codebase:\n")
	b.WriteString("  - Allows engineers to avoid reading the body of the function.\n")
	b.WriteString("  - Allows engineers to understand the shape of the data without reading code (ex: what manner of values might go in a string variable?).\n")
	b.WriteString("  - Correct and error-free with minimal ambiguity.\n")
	b.WriteString("  - Maximize information not present in the code of the snippet (ex: `func (c Company) AddPerson(p Person) // AddPerson adds a person` contains NO new information in the documentation).\n")
	b.WriteString("  - Often shorter than the function itself (otherwise, it won't save an engineer time vs reading the code).\n")
	b.WriteString("- If both options are ~equal in usefulness (ex: they only differ by a few unimportant words), the best option is neither A nor B is best - use \"best\": \"\".\n")
	b.WriteString("  - If you find yourself thinking, 'Option A and B are almost identical, but A has slightly clearer wording...', then use \"best\": \"\".\n")
	b.WriteString("- Non factors:\n")
	b.WriteString("  - Line length (ex: fitting within 80 columns). Line length DOES NOT MATTER AT ALL.\n")
	b.WriteString("  - Minor typos and grammar issues, provided they do not impede usefulness.\n")
	b.WriteString("\n")

	b.WriteString("## Example\n")
	b.WriteString("\n")
	b.WriteString("Input:\n")
	b.WriteString("\n")
	b.WriteString("*Reader.ReadByte:\n")
	b.WriteString("A:\n")
	b.WriteString("```go\n")
	b.WriteString("// ReadByte reads and retrns a single byte. If no byte is available, returns an error.\n")
	b.WriteString("func (b *Reader) ReadByte() (byte, error)\n")
	b.WriteString("```\n")
	b.WriteString("\n")
	b.WriteString("B:\n")
	b.WriteString("```go\n")
	b.WriteString("// ReadByte reads a byte, possibly returning an error.\n")
	b.WriteString("func (b *Reader) ReadByte() (byte, error)\n")
	b.WriteString("```\n")
	b.WriteString("\n")
	b.WriteString("Output:\n")
	b.WriteString("{\n")
	b.WriteString("  \"*Reader.ReadByte\": {\"best\": \"A\", \"reason\": \"A, despite a misspelling, indicates the reason for returning an error, and also states that the byte is returned.\"}\n")
	b.WriteString("}\n")
	b.WriteString("\n")

	return b.String()
}
