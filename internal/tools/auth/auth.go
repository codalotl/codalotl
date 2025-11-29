// auth is just an interface that tools can be built against with minimal dependencies.
//
// NOTE: 2025-11-13: it is not obvious to me why thihs needs to be a separate package vs just putting all the auth code here. Once things settle down,
// if there's no bad deps in the impl, I think it makes sense to just merge these.
//
// NOTE: 2025-11-13 (later that day): I was going to add things like SandboxDir() to this, but I realize that sandboxDir is injected in, eg, IsAuthorizedForRead.
// If I have time, I think i'd like to rename this to authdomain, to unify the concept of auth+domain (sandbox, code unit domain, etc). This would remove sandboxDir from the
// IsAuthorizedForRead, add it to the constructor, and add the below methods in some form.
//   - IsCodeUnitDomain() bool
//   - WithoutCodeUnit() Authorizer
//   - SandboxDir() string
//   - CodeUnitDir() string
package auth

// An Authorizer can answer whether a tool is allowed to be used with respect to a number of paths and parameters.
//
// Authorizers accept an optional requestPermission flag, with an optional reason. If requestPermission=true, the LLM is specifically requesting permission
// to do the operation. This permits the implementation of polices where:
//   - Requests that are normally authorized can get the user permission (ex: reading .env, perhaps)
//   - Requests that are normally denied can requested from the user with a reason.
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
	// IsAuthorizedForRead returns nil if all absPath are authorized to be read wrt the sandboxDir.
	// It returns an error otherwise, where the error explains why authorization was denied.
	IsAuthorizedForRead(requestPermission bool, requestReason string, toolName string, sandboxDir string, absPath ...string) error

	// IsAuthorizedForWrite returns nil if all absPath are authorized to be written wrt the sandboxDir.
	// It returns an error otherwise, where the error explains why authorization was denied.
	IsAuthorizedForWrite(requestPermission bool, requestReason string, toolName string, sandboxDir string, absPath ...string) error

	// IsShellAuthorized returns nil if the shell command is authorized; otherwise, the error explains why authorization was denied.
	IsShellAuthorized(requestPermission bool, requestReason string, sandboxDir string, cwd string, command []string) error
}
