# authdomain

This package defines and builds `Authorizer`s, which implement policies to allow, block, or ask for a read or write request that an Agent/LLM might make. `Authorizer`s are passed
to tool creation functions (ex: `coretools.NewReadFileTool`), so that the tool can check authorization before it (for instance) reads a file.

There are two layers:
- Regular sandbox authorizer (limits reads/writes to a specified sandbox dir). Also limits which shell commands can be run.
- Code unit authorization - additionally blocks reads/writes except for code unit directories (ex: a Go package). Falls back to the regular sandbox authorizer.

## Domains

- Every authorizer is created with a sandbox root. Authorization checks are scoped to that configured sandbox root.
- Code-unit scoping (`NewCodeUnitAuthorizer`) is optional and implemented as a wrapper authorizer. When present:
    - `IsCodeUnitDomain()` returns true.
    - `CodeUnitDir()` returns the code-unit base dir.
    - `WithoutCodeUnit()` returns the underlying sandbox authorizer so callers can drop the code-unit jail when they need to spawn tools that work outside of it.
- Code-unit scoping limits access to certain code units (ex: only read/write in a single Go package). See CodeUnit below.
- Non-code-unit authorizers return `IsCodeUnitDomain() == false`, `CodeUnitDir() == ""`, and `WithoutCodeUnit()` returns itself.

## Policies

### Sandbox

- Allow sandbox reads/writes.
- If operations inside the sandbox requestPermission, prompt.
- Deny anything outside the sandbox root, even if requestPermission.
- Shell:
    - The working directory must be inside the sandbox root (deny otherwise).
    - Consults `ShellAllowedCommands`: allow safe commands (unless requestPermission, then prompt), block blocked commands, and prompt for dangerous/inscrutable/none commands.

### Permissive Sandbox

- Allow sandbox reads/writes.
- Ask user any time requestPermission is true.
- Ask user about operating outside the sandbox root.
- Shell:
    - If cwd is outside the sandbox, always prompt (even if the command is otherwise safe).
    - Consults `ShellAllowedCommands`: allow safe/none commands when requestPermission is false and cwd is inside the sandbox; block blocked commands; prompt for dangerous/inscrutable commands.

### AutoApprove

- Allow everything, no matter what. The user is asked nothing. No shell commands are blocked.

### CodeUnit

- For the {"read_file", "ls", "diagnostics", "run_tests"} tools only, blocks all read paths not in the code
  unit. Blocks all write paths from any tool that are not in the code unit.
    - Shell authorizations are never blocked (we cannot reliably detect paths there right now).
    - Never asks the user permission, even if requestPermission.
- Otherwise, delegates to the fallback Authorizer.
- In other words, this authorizer is strictly more restrictive - it never allows new things, but might flatly block
  reads/writes that the fallback would allow or ask about.

## Grants

Grants enable an agent to access files they wouldn't otherwise have access to. For example: if the end-user creates a code unit authorizer at `internal/foo` and prompts "Read @README.md, then
implement this package", it would allow an agent to issue a `read_file` tool call for `README.md`, even though normally that's outside of the code unit jail. Grants also allow accessing files
outside of the sandbox dir when using the permissive sandbox policy (strict sandbox cannot grant outside of the sandbox dir).

Design choice: we choose to be permissive and allow access to files, even if that wasn't the user's intention. In practice, given "myname@gmail.com", an LLM will not try to read a `gmail.com` file,
and that file likely doesn't exist either. In the case of code units, code units are more about keeping the LLM focused on a package (they're not security). Folks who care about security should
run agents in a real sandbox.

Details:
- **For the time being, this applies to reading via the `read_file` and `ls` tools only.** (This can be extended/relaxed in the future, if needed).
- If a grant applies, the user is never asked for permission, even if requestPermission.
- Grants can be Go-style `filepath.Match` globs.
- If a directory is granted without globs (no `filepath.Match` metacharacters), the grant applies to the entire tree of files/directories rooted at the directory (recursively).
    - Yes, this requirement changes authorizers from mostly pure functions over path strings to requiring `os.Stat`.
- To grant access to files in a directory non-recursively, use, e.g., `@src/*`.
- Grant paths are relative to the sandbox dir, or absolute.
- Strict sandbox: grants never authorize reads outside of the sandbox dir.
- Permissive sandbox: grants may authorize reads outside of the sandbox dir.
- Grants can accumulate in an agent session (the agent must replace the authorizer in a `/new` session).
- The directory `/` cannot be granted (ex: `in @/ myfile.txt, please...`).

`AddGrantsFromUserMessage` is lazy:
- Because user messages are messy and inexact, adding grants is **lazy**. We cannot immediately parse the message to extract grants.
- Instead, if we get `IsAuthorizedForRead` for a specific file, for instance, we examine the user message grants and see if the file is granted by the message.
- This allows messages like `Read @README.md. Then ...` to accept either `README.md` or `README.md.` as grants.
    - Similarly, `in @my file.txt, read the docs` allows files `my`, `my file`, `my file.txt`, and others.
- No `os.Stat` may occur during AddGrantsFromUserMessage.

## User permission requests

Each authorizer constructor that can prompt returns a non-nil, buffered (<-chan UserRequest) that carries
pending approval work. When IsAuthorized* needs human input it enqueues the UserRequest and then blocks on an
internal result channel until Allow/Disallow fires. The authorization call itself is synchronous: IsAuthorized*
only returns once the user has answered or Close runs.

Example usage:

```go
authorizer, userRequests, err := NewSandboxAuthorizer(sandboxDir, commands)
// pass `authorizer` to new tool creation
// ...

// Main event loop:
for {
    select {
    case req, ok := <-userRequests:
        if !ok {
            return // authorizer closed; exit the loop
        }
        fmt.Println(req.Prompt)
        answer := readYesNoFromUser()
        if answer {
            req.Allow()
        } else {
            req.Disallow()
        }
    case evt := <-events: // events from agent package. ex: assistant message
        handle(evt)
    }
}
```

## Public API

```go
func NewSandboxAuthorizer(sandboxDir string, commands *ShellAllowedCommands) (Authorizer, <-chan UserRequest, error)
func NewPermissiveSandboxAuthorizer(sandboxDir string, commands *ShellAllowedCommands) (Authorizer, <-chan UserRequest, error)
func NewAutoApproveAuthorizer(sandboxDir string) Authorizer
func NewCodeUnitAuthorizer(unit *codeunit.CodeUnit, fallback Authorizer) Authorizer


// AddGrantsFromUserMessage adds grants from userMessage to the authorizer. Grants in userMessage are of the form `@relative/path/to/file`, `@/path/to/file`, or `@"path with spaces"`. Note that
// userMessage is a full message typed by the user to the agent, and may contain no grants, errant `@` signs, bad syntax, commas or other punctuation after the grant, and so on.
// This means AddGrantsFromUserMessage needs to robustly handle anything the user may type, and may not know **at the time of calling** what grants are actually being made.
//
// The grants are added to the authorizer as well as its fallback, if present.
// Note: strict sandbox authorizers never allow grants to authorize reads outside of their sandbox dir.
//
// An error is only returned if authorizer is not capable of accepting grants. Any other "errors" simply result in no grants being added (ex: file doesn't exist; bad glob format).
func AddGrantsFromUserMessage(authorizer Authorizer, userMessage string) error

// ShellAllowedCommands keeps track of blocked, dangerous, and safe shell commands. All methods are thread-safe.
//
// The zero value ShellAllowedCommands{} has empty lists.
type ShellAllowedCommands struct {
    // ...
}

type CommandMatcher struct {
    Command string             // main command. ex: "go"
    CommandArgsPrefix []string // exact matches for the rest of argv. ex: []string{"test"} matches `go test .`, provided Command is "go", but does not match `go help test`.
    Flags []string             // ex: "--global" matches any of argv being "--global" or being prefixed with "--global=" or "--global "
}

// NewShellAllowedCommands creates a new ShellAllowedCommands with default blocked/dangerous/safe lists.
func NewShellAllowedCommands() *ShellAllowedCommands


// Hard-coded list of BlockedCommandMatchers that are blocked. Automatically added in NewShellAllowedCommands.
// Might contain things like {"brew", nil, nil} to disallow homebrew access.
func (s *ShellAllowedCommands) DefaultBlockedCommandMatchers() []CommandMatcher

func (s *ShellAllowedCommands) DefaultDangerousCommandMatchers() []CommandMatcher
func (s *ShellAllowedCommands) DefaultSafeCommandMatchers() []CommandMatcher

// Returns all currently blocked command matchers.
func (s *ShellAllowedCommands) BlockedCommandMatchers() []CommandMatcher

func (s *ShellAllowedCommands) DangerousCommandMatchers() []CommandMatcher
func (s *ShellAllowedCommands) SafeCommandMatchers() []CommandMatcher

// Filters currently blocked command matchers by those commands that are just completely blocked regardless of arguments.
// ex: "brew", "apt". If args or flags are set in the matcher, the command is NOT blocked here.
// This is here specifically so we can give an LLM a simple list of blocked commands.
func (s *ShellAllowedCommands) BlockedCommands() []string

// Block blocks m. No-op if m is already blocked.
func (s *ShellAllowedCommands) AddBlocked(m CommandMatcher)

// RemoveBlocked unblocks m.
func (s *ShellAllowedCommands) RemoveBlocked(m CommandMatcher) error

func (s *ShellAllowedCommands) AddDangerous(m CommandMatcher)
func (s *ShellAllowedCommands) RemoveDangerous(m CommandMatcher) error
func (s *ShellAllowedCommands) AddSafe(m CommandMatcher)
func (s *ShellAllowedCommands) RemoveSafe(m CommandMatcher) error

// Check checks arg lexically against the blocked/dangerous/safe commands. It returns a CommandCheckResult (eg, blocked, safe, dangerous, inscrutable, none), or an error.
//   - Precedence: safe > blocked > dangerous. So a match on the safe list overrules everything.
//   - This implies there's no super-clean way to specify "allow go commands, except for go install". You'd need to either explicitly enumerate safe go commands, or not add go to the safe list.
//   - inscrutable is returned if we detect argv contains a set of pipes, subshells, xargs, or various other non-simple elements, which we don't support reasoning about.
//   - a command is marked as dangerous if it does not match any list and argv[0] is a path-qualified command (absolute or uses ".."); this is treated as "outside sandbox" heuristically.
//   - a scrutable command that is on no list is 'none'.
func (s *ShellAllowedCommands) Check(argv []string) (CommandCheckResult, error)

// Allow/Disallow should be invoked exactly once; they are internally guarded so extra calls are cheap no-ops. The caller
// choosing neither leaves the underlying authorization call blocked until Close is invoked.
type UserRequest struct {
    ToolCallID string            // The related tool call ID, if available
    ToolName   string            // Name of the tool. Should match a tool's Name() value
    Prompt     string            // human-readable question to surface to the user.
    Argv       []string          // argv of the shell command to allow; nil unless a shell request
    Allow      func()            // idempotent; unblocks the pending authorization with an "allow".
    Disallow   func()            // idempotent; unblocks the pending authorization with a "deny".
    // private fields ok
}

// An Authorizer can answer whether a tool is allowed to be used with respect to a number of paths and parameters.
//
// Authorizers accept an optional requestPermission flag, with an optional reason. If requestPermission=true, the LLM is specifically requesting permission
// to do the operation. This permits the implementation of policies where:
//   - Requests that are normally authorized can get the user permission (ex: reading .env, perhaps)
//   - Requests that are normally denied can be requested from the user with a reason.
//   - Of course, the authorizer is free to disregard this param (auto-approve-all, or deny-all-outside of sandbox).
//
// Implementors may implement pure functions over these params (for instance, implementing policies like "never r/w outside of sandbox"),
// or they may base their answer on actual contents of the file system (for instance, pre-opening a file and checking to see if it has secrets).
// They may also decide to base their answer at any time on synchronous user input (ex: Do you want to allow Read of some/file? Yes or No).
//
// Note that even if Authorizer returns nil, actual filesystem permissions or OS-level sandboxing may prevent a read or write.
//
// Finally, a design note: we're not passing the tool call itself in, nor the raw parameters. Ideally, an Authorizer shouldn't need to know
// about specific tools or their implementation.
type Authorizer interface {
    // SandboxDir returns the normalized sandbox root for this authorizer.
    SandboxDir() string

    // CodeUnitDir returns the code unit base dir if a code unit domain is active, else "".
    CodeUnitDir() string

    // IsCodeUnitDomain reports whether this authorizer enforces a code unit domain.
    IsCodeUnitDomain() bool

    // WithoutCodeUnit returns an authorizer with code-unit restrictions removed (typically the fallback sandbox authorizer).
    WithoutCodeUnit() Authorizer

    // IsAuthorizedForRead returns nil if all absPath are authorized to be read.
    // It returns an error otherwise, where the error explains why authorization was denied.
    IsAuthorizedForRead(requestPermission bool, requestReason string, toolName string, absPath ...string) error

    // IsAuthorizedForWrite returns nil if all absPath are authorized to be written.
    // It returns an error otherwise, where the error explains why authorization was denied.
    IsAuthorizedForWrite(requestPermission bool, requestReason string, toolName string, absPath ...string) error

    // IsShellAuthorized returns nil if the shell command is authorized; otherwise, the error explains why authorization was denied.
    IsShellAuthorized(requestPermission bool, requestReason string, cwd string, command []string) error

    // Close will close all channels and do any other cleanup. Cause any outstanding user requests to auto-disallow.
    Close()
}
```
