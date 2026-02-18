# detectlang

The detectlang package detects the programming language used in files or in filesystems.

## Mechanism

This package ONLY uses file extensions to determine language.

Future improvements:
- May look at bytes of files to disambiguate.
- May use manifest-type files (ex: go.mod, Cargo.toml) in some way.
- May use LLMs?

## Public API

```go
type Lang string

const (
	LangUnknown  Lang = ""
	LangMultiple Lang = "multiple"
	LangGo       Lang = "go"
	LangRuby     Lang = "rb"
	LangPython   Lang = "py"
	LangRust     Lang = "rs"
	// etc
)

// Detect detects the programming language indicated by absPath (which must be absolute). The path can either be to a file or a directory.
//   - When used on a file, only looks at that file. Returns LangUnknown or a specific language. Never LangMultiple. Otherwise...
//   - If the directory has a dominant language by plurality, we return that language (ex: a bunch of .go files and one .rb file). Otherwise...
//   - If the directory has an equal amount of some set of languages, return LangMultiple.
//   - If the directory has no files at all, or no files with a known extension, iteratively check nearby dirs in BFS manner (ex: parent, children, parent's parent,
//     children's children, parent's children, etc). Stop when we get language that is not LangUnknown. If the BFS exhausts without finding a known language, return
//     LangUnknown. Does not traverse beyond absRootDir.
//
// An error is returned if absPath isn't absolute or not in absRootDir, if the path does not exist, or if some other I/O error occurs.
func Detect(absRootDir, absPath string) (Lang, error)
```
