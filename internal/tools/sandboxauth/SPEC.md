# sandboxauth

This package implements an `auth.Authorizer` interface. Agents will use this package by grabbing a concrete Authorizer, then creating their tools with them.

## Policies

### Sandbox

- Only read and write in the sandbox.
- If operations in the sandbox requestPermission, we still ask for it.
- Deny anything outside sandbox, even if requestPermission.
- Consults ShellAllowedCommands - allows the safe commands, blocks the blocked ones, asks about dangerous ones, in that order. Inscrutable: ask; none: ask

### Permissive Sandbox

- Allow sandbox.
- Ask user any time requestPermission is true.
- Ask user about operating outside the sandbox (ex: read or write a file outside of sandbox).
- Consults ShellAllowedCommands - allows the safe commands, blocks the blocked ones, asks about dangerous ones, in that order. Inscrutable: ask; none: allow.

### AutoApprove

- Allow everything, no matter what. The user is asked nothing. No shell commands are blocked.

### CodeUnit

- For the {"read_file", "ls", "diagnostics", "run_tests"} tools only, blocks all read paths not in the code unit. Blocks all write paths from any tool that are not in the code unit.
    - Shell authorizations are never blocked (we can't reliably detect paths there right now).
    - Never directly asks the user permission, even if requestPermission.
- Otherwise, delegates to the fallback Authorizer (which may ask the user permission).
- In other words, this authorizer is strictly more strict - it never allows new things, but might flatly block reads/write that the fallback would allow.

## User permission requests

Each authorizer constructor returns a non-nil, buffered (<-chan UserRequest) that carries pending approval work.
When IsAuthorized* needs human input it enqueues the UserRequest and then blocks on an internal result channel until
Allow/Disallow fires. The authorization call itself is synchronous: IsAuthorized* only returns once the user has answered or Close runs.


Example usage:

```go
authorizer, userRequests, err := NewSandboxAuthorizer(commands)
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
func NewSandboxAuthorizer(commands *ShellAllowedCommands) (Authorizer, <-chan UserRequest, error) 
func NewPermissiveSandboxAuthorizer(commands *ShellAllowedCommands) (Authorizer, <-chan UserRequest, error) 
func NewAutoApproveAuthorizer() Authorizer
func NewCodeUnitAuthorizer(unit *codeunit.CodeUnit, fallback Authorizer) Authorizer


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

// Block blocks m. No-op if m was already blocked.
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
//   - a command is marked as dangerous if doesn't match any list, and it's path is qualified and falls outside of sandbox dir. Ex: "../../../bin/rm" or "/usr/local/bin/go"
//   - a scrutable command that is on no list is 'none'.
func (s *ShellAllowedCommands) Check(argv []string) (CommandCheckResult, error)

// Allow/Disallow should be invoked exactly once; they are internally guarded so extra calls are cheap no-ops. The caller
// choosing neither leaves the underlying authorization call blocked until Close is invoked.
type UserRequest struct {
    ToolCallID string            // The related tool call ID
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
	auth.Authorizer

    // Close will close all channels and do any other cleanup. Cause any outstanding user requests to auto-disallow.
    // NOTE: this isn't part of the tools package Authorizer.
    Close()
}
```
