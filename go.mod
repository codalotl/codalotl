module github.com/codalotl/codalotl

go 1.24.4

// All deps from 'golang.org/x/*' are considered 'free':
require (
	golang.org/x/mod v0.30.0
	golang.org/x/sys v0.38.0
	golang.org/x/term v0.37.0
	golang.org/x/tools v0.38.0
)

// Dependencies we're okay with:
// (these deps must have 0 transitive deps and/or be difficult to reproduce)
require (
	github.com/clipperhouse/uax29/v2 v2.2.0 // 0 transitive deps!
	github.com/mattn/go-runewidth v0.0.19 // 1 transitive dep: github.com/clipperhouse/uax29
	github.com/yuin/goldmark v1.7.13 // 0 transitive deps!
)

// Dependencies we're on the fence about:
// (these deps often have good value, but come with too many transitive deps)
require (
	github.com/openai/openai-go/v3 v3.7.0 // MANY transitive deps :(
	github.com/sergi/go-diff v1.4.0 // only deps are testify->many things
	github.com/stretchr/testify v1.11.1 // MANY transitive deps :(
)

// Dependencies we want to delete:
require github.com/tiktoken-go/tokenizer v0.6.2 // 1 transitive dep on github.com/dlclark/regexp2. But, I don't think we need this functionality at all.

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/dlclark/regexp2 v1.11.5 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/tidwall/gjson v1.14.4 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
	golang.org/x/sync v0.17.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
