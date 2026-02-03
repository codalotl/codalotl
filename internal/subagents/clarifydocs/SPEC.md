# clarifydocs

The clarifydocs package is a SubAgent that will get clarification on documentation about a public API.

This package must be at least functional for all languages. Certain languages may have extra support that increases performance for that language (ex: Go might receive AST-aware helpers).

## Dependencies

- `codeai/tools/coretools`
- `codeai/tools/authdomain`
- `codeai/agent`
- `codeai/initialcontext`
- `codeai/detectlang`
- `q/cmdrunner`

## Tools

This SubAgent will have access to these tools:
- `codeai/tools/coretools`: `ls`
- `codeai/tools/coretools`: `read_file`

Notably, these tools are absent: `apply_patch` (obviously, since this is just a read-only agent), `shell`.

## Prompt and Context

- Prompt: the generic prompt our agents use (codeai/prompt package).
- Initial context:
    - Non-Go: the results of a recursive `rg` on `identifier` within the directory that contains path (or within path itself if path is a directory).
    - Go: `codeai/initialcontext.Create`.

NOTE: in the future, we may want to give the LLM a `find` tool to execute more `rg` commands for languages without enhanced support.

## Public API

```go {api exact_docs}
// ClarifyAPI clarifies the API/docs for identifier found in path and returns an answer. An error is returned for invalid inputs, failure to communicate with the LLM, etc.
// If the LLM can't find the identifier as it relates to path, it may say so in the answer, which doesn't produce an error.
//   - sandboxAbsDir is used for tool construction and relative path resolution, not as a confinement mechanism.
//   - authorizer is optional. If present, it confines the SubAgent in some way (usually to a sandbox dir of some kind).
//   - toolset toolsetinterface.Toolset are the tools available for use. Injected to cut dependencies. Should be ls/read_file.
//   - path is absolute or relative to sandboxAbsDir. If absolute, it may be outside of sandboxAbsDir (for instance, when clarifying dep packages or stdlib packages).
//   - identifier is language-specific and opaque. For Go, it looks like "MyVar", "*MyType.MyFunc", etc.
//
// When clarifying a dep package outside of the sandbox, and authorizer is not nil, it is recommended for UX reasons (but not required) to construct an authorizer to allow reads. There are many
// ways to do this - one is to create a new authorizer with sandbox root of the dep; another is to add a 'grant' to the authorizer.
//
// Example question: "What does the first return parameter (a string) look like in the ClarifyAPI func?". Example answer that might be returned: "The ClarifyAPI func
// returns a human- or LLM-readable answer to the specified question. It will be the empty string if an error occurred."
func ClarifyAPI(ctx context.Context, agentCreator agent.AgentCreator, sandboxAbsDir string, authorizer authdomain.Authorizer, toolset toolsetinterface.Toolset, path string, identifier string, question string) (string, error)
```
