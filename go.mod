module github.com/codalotl/codalotl

go 1.24.4

require (
    // All deps from 'golang.org/x/*' are considered 'free':
    golang.org/x/sys v0.38.0
	golang.org/x/term v0.37.0

	// Dependencies we're okay with:
	github.com/clipperhouse/uax29/v2 v2.2.0
	github.com/mattn/go-runewidth v0.0.19
	github.com/stretchr/testify v1.11.1
	
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
