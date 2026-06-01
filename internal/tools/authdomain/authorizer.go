// Package authdomain defines authorization policies for tools that read, write, or run shell commands.
//
// Authorizers always have a sandbox root. A code-unit authorizer is an optional wrapper around another Authorizer that adds a narrower filesystem domain before
// delegating to the wrapped authorizer's sandbox policy.
package authdomain

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/codalotl/codalotl/internal/codeunit"
)

// ErrAuthorizerClosed is returned when an authorization request is made after Close.
var ErrAuthorizerClosed = errors.New("authdomain: authorizer closed")

// ErrAuthorizationDenied is returned when the user declines a pending authorization request.
var ErrAuthorizationDenied = errors.New("authdomain: authorization denied")

// ErrCodeUnitPathOutside is returned when a code-unit domain denies access because a path is outside the code unit.
//
// Use errors.Is(err, ErrCodeUnitPathOutside) to detect this condition.
var ErrCodeUnitPathOutside = errors.New("authdomain: path outside code unit")

const userRequestBufferSize = 16

// Allow/Disallow should be invoked exactly once; they are internally guarded so extra calls are cheap no-ops. The caller choosing neither leaves the underlying
// authorization call blocked until Close is invoked.
type UserRequest struct {
	ToolCallID string   // The related tool call ID, if available
	ToolName   string   // Name of the tool. Should match a tool's Name() value
	Prompt     string   // human-readable question to surface to the user.
	Argv       []string // argv of the shell command to allow; nil unless a shell request
	Allow      func()   // idempotent; unblocks the pending authorization with an "allow".
	Disallow   func()   // idempotent; unblocks the pending authorization with a "deny".

	// private fields ok
}

// An Authorizer can answer whether a tool is allowed to be used with respect to a number of paths and parameters.
//
// Authorizers accept an optional requestPermission flag, with an optional reason. If requestPermission=true, the LLM is specifically requesting permission to do
// the operation. This permits the implementation of policies where:
//   - Requests that are normally authorized can get the user permission (ex: reading .env, perhaps)
//   - Requests that are normally denied can be requested from the user with a reason.
//   - Of course, the authorizer is free to disregard this param (auto-approve-all, or deny-all-outside of sandbox).
//
// Implementors may implement pure functions over these params (for instance, implementing policies like "never r/w outside of sandbox"), or they may base their
// answer on actual contents of the file system (for instance, pre-opening a file and checking to see if it has secrets). They may also decide to base their answer
// at any time on synchronous user input (ex: Do you want to allow Read of some/file? Yes or No).
//
// Note that even if Authorizer returns nil, actual filesystem permissions or OS-level sandboxing may prevent a read or write.
//
// Finally, a design note: we're not passing the tool call itself in, nor the raw parameters. Ideally, an Authorizer shouldn't need to know about specific tools
// or their implementation.
type Authorizer interface {
	// SandboxDir returns the normalized sandbox root for this authorizer.
	//
	// For a code-unit authorizer, this is the wrapped fallback authorizer's sandbox, not the code unit directory.
	SandboxDir() string

	// CodeUnitDir returns the code unit base dir if a code unit domain is active, else "".
	CodeUnitDir() string

	// IsCodeUnitDomain reports whether this authorizer enforces a code unit domain.
	IsCodeUnitDomain() bool

	// WithoutCodeUnit returns an authorizer with code-unit restrictions removed.
	//
	// For a code-unit authorizer, this is the wrapped fallback authorizer. For other authorizers, it is the receiver.
	WithoutCodeUnit() Authorizer

	// IsAuthorizedForRead returns nil if all absPath are authorized to be read. It returns an error otherwise, where the error explains why authorization was denied.
	IsAuthorizedForRead(requestPermission bool, requestReason string, toolName string, absPath ...string) error

	// IsAuthorizedForWrite returns nil if all absPath are authorized to be written. It returns an error otherwise, where the error explains why authorization was denied.
	IsAuthorizedForWrite(requestPermission bool, requestReason string, toolName string, absPath ...string) error

	// IsShellAuthorized returns nil if the shell command is authorized; otherwise, the error explains why authorization was denied.
	IsShellAuthorized(requestPermission bool, requestReason string, cwd string, command []string) error

	// Close will close all channels and do any other cleanup. Cause any outstanding user requests to auto-disallow.
	Close()
}

// NewSandboxAuthorizer constructs an Authorizer implementing the Sandbox policy.
func NewSandboxAuthorizer(sandboxDir string, commands *ShellAllowedCommands) (Authorizer, <-chan UserRequest, error) {
	if commands == nil {
		commands = NewShellAllowedCommands()
	}

	sandbox, err := normalizeSandboxDir(sandboxDir)
	if err != nil {
		return nil, nil, err
	}

	base := newBaseAuthorizer()
	auth := &sandboxAuthorizer{
		baseAuthorizer: base,
		sandboxDir:     sandbox,
		commands:       commands,
	}
	return auth, base.requests, nil
}

// NewPermissiveSandboxAuthorizer constructs an Authorizer implementing the Permissive Sandbox policy.
func NewPermissiveSandboxAuthorizer(sandboxDir string, commands *ShellAllowedCommands) (Authorizer, <-chan UserRequest, error) {
	if commands == nil {
		commands = NewShellAllowedCommands()
	}

	sandbox, err := normalizeSandboxDir(sandboxDir)
	if err != nil {
		return nil, nil, err
	}

	base := newBaseAuthorizer()
	auth := &permissiveSandboxAuthorizer{
		baseAuthorizer: base,
		sandboxDir:     sandbox,
		commands:       commands,
	}
	return auth, base.requests, nil
}

// NewAutoApproveAuthorizer constructs the AutoApprove policy Authorizer.
func NewAutoApproveAuthorizer(sandboxDir string) Authorizer {
	sandbox, err := normalizeSandboxDir(sandboxDir)
	if err != nil {
		panic(fmt.Sprintf("authdomain: invalid sandbox dir: %v", err))
	}
	return autoApproveAuthorizer{sandboxDir: sandbox}
}

// NewSessionAuthorizer constructs the standard codalotl session authorizer.
//
// When autoApprove is false, this uses the permissive sandbox policy and returns the user request channel for interactive approvals. When autoApprove is true, this
// uses the auto-approve policy and returns a nil request channel because no approval prompts can be emitted.
func NewSessionAuthorizer(sandboxDir string, commands *ShellAllowedCommands, autoApprove bool) (Authorizer, <-chan UserRequest, error) {
	if autoApprove {
		sandbox, err := normalizeSandboxDir(sandboxDir)
		if err != nil {
			return nil, nil, err
		}
		return autoApproveAuthorizer{sandboxDir: sandbox}, nil, nil
	}
	return NewPermissiveSandboxAuthorizer(sandboxDir, commands)
}

// NewCodeUnitAuthorizer constructs an Authorizer that enforces membership in unit before delegating to fallback.
//
// The returned authorizer preserves fallback's sandbox policy and adds code-unit checks for filesystem operations. Reads are code-unit restricted for read_file,
// ls, diagnostics, and run_tests. Writes are code-unit restricted for all tools. Shell authorization is delegated directly to fallback because shell command paths
// are not modeled precisely here.
//
// Grants from AddGrantsFromUserMessage can allow read_file and ls to read outside the code unit, subject to fallback's sandbox policy.
func NewCodeUnitAuthorizer(unit *codeunit.CodeUnit, fallback Authorizer) Authorizer {
	if unit == nil {
		panic("authdomain: code unit is nil")
	}
	if fallback == nil {
		panic("authdomain: fallback authorizer is nil")
	}
	return &codeUnitAuthorizer{
		unit:     unit,
		fallback: fallback,
		grants:   newGrantStore(),
	}
}

// WithUpdatedSandbox returns a duplicate of authorizer except with a different sandboxDir. It re-uses the same ShellAllowedCommands, request channel, grants, etc.
//
// This can be used to run subagents in other directories outside the sandbox (e.g., investigating a shared library).
//
// If authorizer has an active code-unit domain, WithUpdatedSandbox preserves that code-unit wrapper and updates only its fallback authorizer. The returned authorizer
// still reports IsCodeUnitDomain() == true, CodeUnitDir() remains the original code unit directory, and filesystem read/write checks still enforce the original
// code-unit restrictions before delegating to the updated fallback. Shell checks continue to delegate to the fallback, so the updated sandbox affects shell cwd
// policy.
//
// To move work outside both the current sandbox and the code unit, first drop the code-unit wrapper:
//
//	updated, err := WithUpdatedSandbox(authorizer.WithoutCodeUnit(), otherDir)
func WithUpdatedSandbox(authorizer Authorizer, sandboxDir string) (Authorizer, error) {
	if authorizer == nil {
		return nil, errors.New("authorizer is nil")
	}

	sandbox, err := normalizeSandboxDir(sandboxDir)
	if err != nil {
		return nil, err
	}

	switch a := authorizer.(type) {
	case *sandboxAuthorizer:
		return &sandboxAuthorizer{
			baseAuthorizer: a.baseAuthorizer,
			sandboxDir:     sandbox,
			commands:       a.commands,
		}, nil
	case *permissiveSandboxAuthorizer:
		return &permissiveSandboxAuthorizer{
			baseAuthorizer: a.baseAuthorizer,
			sandboxDir:     sandbox,
			commands:       a.commands,
		}, nil
	case autoApproveAuthorizer:
		return autoApproveAuthorizer{sandboxDir: sandbox}, nil
	case *autoApproveAuthorizer:
		return &autoApproveAuthorizer{sandboxDir: sandbox}, nil
	case *codeUnitAuthorizer:
		updatedFallback, err := WithUpdatedSandbox(a.fallback, sandbox)
		if err != nil {
			return nil, err
		}
		return &codeUnitAuthorizer{
			unit:     a.unit,
			fallback: updatedFallback,
			grants:   a.grants,
		}, nil
	default:
		return nil, fmt.Errorf("authorizer type %T does not support WithUpdatedSandbox", authorizer)
	}
}

// The sandboxAuthorizer type implements strict sandbox authorization.
type sandboxAuthorizer struct {
	*baseAuthorizer                       // Embedded base authorizer manages approval requests, read grants, and close state.
	sandboxDir      string                // Normalized sandbox root for filesystem paths and shell working directories.
	commands        *ShellAllowedCommands // Shell command policy used by IsShellAuthorized.
}

// SandboxDir returns the normalized sandbox root used by this authorizer.
func (a *sandboxAuthorizer) SandboxDir() string {
	return a.sandboxDir
}

// CodeUnitDir returns an empty string because sandboxAuthorizer does not enforce a code-unit domain.
func (a *sandboxAuthorizer) CodeUnitDir() string {
	return ""
}

// IsCodeUnitDomain reports false because sandboxAuthorizer enforces only the sandbox domain.
func (a *sandboxAuthorizer) IsCodeUnitDomain() bool {
	return false
}

// WithoutCodeUnit returns the receiver because sandboxAuthorizer has no code-unit restrictions.
func (a *sandboxAuthorizer) WithoutCodeUnit() Authorizer {
	return a
}

// IsAuthorizedForRead reports whether all paths may be read under the strict sandbox policy. Paths outside SandboxDir are denied without prompting. Inside paths
// are allowed unless requestPermission is true, in which case ungranted paths require user approval and requestReason is included in the prompt.
func (a *sandboxAuthorizer) IsAuthorizedForRead(requestPermission bool, requestReason string, toolName string, absPath ...string) error {
	if err := a.baseAuthorizer.checkOpen(); err != nil {
		return err
	}
	if len(absPath) == 0 {
		return nil
	}

	sandbox := a.sandboxDir
	inside, outside, err := classifyPaths(sandbox, absPath)
	if err != nil {
		return err
	}

	if len(outside) > 0 {
		return fmt.Errorf("path %q is outside sandbox %q; operating in strict sandbox mode - request denied", outside[0], sandbox)
	}

	if requestPermission && len(inside) > 0 {
		inside = filterGrantedReadPaths(a.baseAuthorizer.grants, sandbox, toolName, false, inside)
		return a.promptForPaths(toolName, "read", scopeInsideSandbox, sandbox, inside, requestReason, true)
	}

	return nil
}

// IsAuthorizedForWrite reports whether all paths may be written under the strict sandbox policy. Paths outside SandboxDir are denied without prompting. Inside paths
// are allowed unless requestPermission is true, in which case the paths require user approval and requestReason is included in the prompt.
func (a *sandboxAuthorizer) IsAuthorizedForWrite(requestPermission bool, requestReason string, toolName string, absPath ...string) error {
	if err := a.baseAuthorizer.checkOpen(); err != nil {
		return err
	}
	if len(absPath) == 0 {
		return nil
	}

	sandbox := a.sandboxDir
	inside, outside, err := classifyPaths(sandbox, absPath)
	if err != nil {
		return err
	}

	if len(outside) > 0 {
		return fmt.Errorf("path %q is outside sandbox %q; operating in strict sandbox mode - request denied", outside[0], sandbox)
	}

	if requestPermission && len(inside) > 0 {
		return a.promptForPaths(toolName, "write", scopeInsideSandbox, sandbox, inside, requestReason, true)
	}

	return nil
}

// IsShellAuthorized reports whether command may run from cwd under the strict sandbox policy. A non-empty cwd is normalized and must be inside SandboxDir. The command
// is classified with ShellAllowedCommands: safe commands are allowed unless requestPermission asks the user, blocked commands are denied, and dangerous, inscrutable,
// or unmatched commands require user approval.
func (a *sandboxAuthorizer) IsShellAuthorized(requestPermission bool, requestReason string, cwd string, command []string) error {
	if err := a.baseAuthorizer.checkOpen(); err != nil {
		return err
	}

	sandbox := a.sandboxDir
	if cwd != "" {
		cleanCwd, err := normalizeAbsolutePath(cwd)
		if err != nil {
			return err
		}
		if !withinSandbox(sandbox, cleanCwd) {
			return fmt.Errorf("cwd %q is outside sandbox %q; operating in strict sandbox mode - request denied", cleanCwd, sandbox)
		}
	}

	result, err := a.commands.Check(command)
	if err != nil {
		return err
	}

	switch result {
	case CommandCheckResultSafe:
		if requestPermission {
			return a.promptForCommand(cwd, command, requestReason, result, true, false)
		}
		return nil
	case CommandCheckResultBlocked:
		return fmt.Errorf("command %q is blocked by policy", strings.Join(command, " "))
	case CommandCheckResultDangerous, CommandCheckResultInscrutable, CommandCheckResultNone:
		return a.promptForCommand(cwd, command, requestReason, result, requestPermission, false)
	default:
		return fmt.Errorf("unknown command check result %d", result)
	}
}

// Close releases shared authorizer resources and unblocks pending user requests. It delegates to baseAuthorizer.Close and is idempotent.
func (a *sandboxAuthorizer) Close() {
	a.baseAuthorizer.Close()
}

// The permissiveSandboxAuthorizer type implements permissive sandbox authorization.
type permissiveSandboxAuthorizer struct {
	*baseAuthorizer                       // Embedded base authorizer manages approval requests, read grants, and close state.
	sandboxDir      string                // Normalized sandbox root used to classify filesystem paths and shell working directories.
	commands        *ShellAllowedCommands // Shell command policy used by IsShellAuthorized.
}

// SandboxDir returns the normalized sandbox root used by this authorizer.
func (a *permissiveSandboxAuthorizer) SandboxDir() string {
	return a.sandboxDir
}

// CodeUnitDir returns an empty string because permissiveSandboxAuthorizer does not enforce a code-unit domain.
func (a *permissiveSandboxAuthorizer) CodeUnitDir() string {
	return ""
}

// IsCodeUnitDomain reports false because permissiveSandboxAuthorizer enforces only the sandbox domain.
func (a *permissiveSandboxAuthorizer) IsCodeUnitDomain() bool {
	return false
}

// WithoutCodeUnit returns the receiver because permissiveSandboxAuthorizer has no code-unit restrictions.
func (a *permissiveSandboxAuthorizer) WithoutCodeUnit() Authorizer {
	return a
}

// IsAuthorizedForRead reports whether all paths may be read under the permissive sandbox policy. Inside paths are allowed unless requestPermission is true. Outside
// paths require user approval unless covered by a read grant, and requestReason is included in any prompt.
func (a *permissiveSandboxAuthorizer) IsAuthorizedForRead(requestPermission bool, requestReason string, toolName string, absPath ...string) error {
	if err := a.baseAuthorizer.checkOpen(); err != nil {
		return err
	}
	if len(absPath) == 0 {
		return nil
	}

	sandbox := a.sandboxDir
	inside, outside, err := classifyPaths(sandbox, absPath)
	if err != nil {
		return err
	}

	if len(outside) > 0 {
		outside = filterGrantedReadPaths(a.baseAuthorizer.grants, sandbox, toolName, true, outside)
		if len(outside) > 0 {
			return a.promptForPaths(toolName, "read", scopeOutsideSandbox, sandbox, outside, requestReason, requestPermission)
		}
	}

	if requestPermission && len(inside) > 0 {
		inside = filterGrantedReadPaths(a.baseAuthorizer.grants, sandbox, toolName, true, inside)
		return a.promptForPaths(toolName, "read", scopeInsideSandbox, sandbox, inside, requestReason, true)
	}

	return nil
}

// IsAuthorizedForWrite reports whether all paths may be written under the permissive sandbox policy. Inside paths are allowed unless requestPermission is true.
// Outside paths require user approval, and requestReason is included in any prompt.
func (a *permissiveSandboxAuthorizer) IsAuthorizedForWrite(requestPermission bool, requestReason string, toolName string, absPath ...string) error {
	if err := a.baseAuthorizer.checkOpen(); err != nil {
		return err
	}
	if len(absPath) == 0 {
		return nil
	}

	sandbox := a.sandboxDir
	inside, outside, err := classifyPaths(sandbox, absPath)
	if err != nil {
		return err
	}

	if len(outside) > 0 {
		return a.promptForPaths(toolName, "write", scopeOutsideSandbox, sandbox, outside, requestReason, requestPermission)
	}

	if requestPermission && len(inside) > 0 {
		return a.promptForPaths(toolName, "write", scopeInsideSandbox, sandbox, inside, requestReason, true)
	}

	return nil
}

// IsShellAuthorized reports whether command may run from cwd under the permissive sandbox policy. A non-empty cwd is normalized; an outside-sandbox cwd requires
// user approval rather than immediate denial. The command is classified with ShellAllowedCommands: safe and unmatched commands are allowed inside the sandbox unless
// requestPermission asks the user, blocked commands are denied, and dangerous or inscrutable commands require user approval.
func (a *permissiveSandboxAuthorizer) IsShellAuthorized(requestPermission bool, requestReason string, cwd string, command []string) error {
	if err := a.baseAuthorizer.checkOpen(); err != nil {
		return err
	}

	sandbox := a.sandboxDir
	cwdOutside := false
	if cwd != "" {
		cleanCwd, err := normalizeAbsolutePath(cwd)
		if err != nil {
			return err
		}
		if !withinSandbox(sandbox, cleanCwd) {
			cwdOutside = true
			cwd = cleanCwd
		} else {
			cwd = cleanCwd
		}
	}

	result, err := a.commands.Check(command)
	if err != nil {
		return err
	}

	switch result {
	case CommandCheckResultSafe:
		if requestPermission || cwdOutside {
			return a.promptForCommand(cwd, command, requestReason, result, requestPermission, cwdOutside)
		}
		return nil
	case CommandCheckResultBlocked:
		return fmt.Errorf("command %q is blocked by policy", strings.Join(command, " "))
	case CommandCheckResultDangerous, CommandCheckResultInscrutable:
		return a.promptForCommand(cwd, command, requestReason, result, requestPermission, cwdOutside)
	case CommandCheckResultNone:
		if requestPermission || cwdOutside {
			return a.promptForCommand(cwd, command, requestReason, result, requestPermission, cwdOutside)
		}
		return nil
	default:
		return fmt.Errorf("unknown command check result %d", result)
	}
}

// Close releases shared authorizer resources and unblocks pending user requests. It delegates to baseAuthorizer.Close and is idempotent.
func (a *permissiveSandboxAuthorizer) Close() {
	a.baseAuthorizer.Close()
}

// autoApproveAuthorizer is an Authorizer implementation that allows every operation without prompting.
type autoApproveAuthorizer struct {
	sandboxDir string // sandboxDir is the normalized sandbox root reported by SandboxDir.
}

// SandboxDir returns the normalized sandbox root configured for this authorizer.
func (a autoApproveAuthorizer) SandboxDir() string {
	return a.sandboxDir
}

// addGrantUserMessage ignores grant messages because auto-approve authorizers already allow all reads.
func (autoApproveAuthorizer) addGrantUserMessage(string) {}

// CodeUnitDir returns an empty string because auto-approve authorization is not scoped to a code unit.
func (autoApproveAuthorizer) CodeUnitDir() string {
	return ""
}

// IsCodeUnitDomain reports false because auto-approve authorization is not scoped to a code unit.
func (autoApproveAuthorizer) IsCodeUnitDomain() bool {
	return false
}

// WithoutCodeUnit returns the auto-approve authorizer unchanged because it has no code-unit wrapper.
func (a autoApproveAuthorizer) WithoutCodeUnit() Authorizer {
	return a
}

// IsAuthorizedForRead always authorizes reads without prompting or inspecting paths.
func (autoApproveAuthorizer) IsAuthorizedForRead(bool, string, string, ...string) error {
	return nil
}

// IsAuthorizedForWrite always authorizes writes without prompting or inspecting paths.
func (autoApproveAuthorizer) IsAuthorizedForWrite(bool, string, string, ...string) error {
	return nil
}

// IsShellAuthorized always authorizes shell execution without prompting or inspecting the command.
func (autoApproveAuthorizer) IsShellAuthorized(bool, string, string, []string) error {
	return nil
}

// Close releases no resources because auto-approve authorizers do not create pending requests.
func (autoApproveAuthorizer) Close() {}

// codeUnitAuthorizer restricts filesystem authorization to a code unit before delegating to a fallback Authorizer.
//
// It applies code-unit checks to writes for all tools and to reads for tools that require strict reads. Shell authorization is delegated unchanged because shell
// command paths are not modeled precisely.
type codeUnitAuthorizer struct {
	unit     *codeunit.CodeUnit // Unit is the code unit whose membership is enforced.
	fallback Authorizer         // Fallback is the underlying authorizer that applies sandbox policy and user prompts.
	grants   *grantStore        // Grants stores lazy read grants extracted from user messages.
}

// addGrantUserMessage records a user grant message for the code-unit authorizer when grant storage is available.
func (a *codeUnitAuthorizer) addGrantUserMessage(userMessage string) {
	if a.grants == nil {
		return
	}
	a.grants.addGrantUserMessage(userMessage)
}

// SandboxDir returns the sandbox root reported by the fallback authorizer.
func (a *codeUnitAuthorizer) SandboxDir() string {
	return a.fallback.SandboxDir()
}

// CodeUnitDir returns the active code unit's base directory.
func (a *codeUnitAuthorizer) CodeUnitDir() string {
	return a.unit.BaseDir()
}

// IsCodeUnitDomain reports that this authorizer enforces a code-unit domain.
func (a *codeUnitAuthorizer) IsCodeUnitDomain() bool {
	return true
}

// WithoutCodeUnit returns the fallback authorizer with code-unit restrictions removed.
func (a *codeUnitAuthorizer) WithoutCodeUnit() Authorizer {
	return a.fallback
}

var codeUnitStrictReadToolNames = []string{"read_file", "ls", "diagnostics", "run_tests"}

func toolRequiresStrictReads(toolName string) bool {
	return slices.Contains(codeUnitStrictReadToolNames, toolName)
}

// IsAuthorizedForRead authorizes read access under the active code-unit policy.
//
// Tools that require strict code-unit reads must request paths inside the code unit, unless the tool supports read grants and a grant applies. The code-unit check
// itself does not prompt; after it passes, authorization is delegated to the fallback authorizer. A rejected code-unit path returns an error matching ErrCodeUnitPathOutside.
func (a *codeUnitAuthorizer) IsAuthorizedForRead(requestPermission bool, requestReason string, toolName string, absPath ...string) error {
	if toolRequiresStrictReads(toolName) {
		if toolAllowsReadGrants(toolName) {
			if err := a.ensurePathsIncludedOrGranted(toolName, absPath...); err != nil {
				return err
			}
		} else {
			if err := a.ensurePathsIncluded(absPath...); err != nil {
				return err
			}
		}
	}
	return a.fallback.IsAuthorizedForRead(requestPermission, requestReason, toolName, absPath...)
}

// IsAuthorizedForWrite authorizes writes only when every path is inside the active code unit and the fallback authorizer also allows the write.
//
// Code-unit rejection is immediate and does not request user approval; requestPermission and requestReason are passed only to the fallback after the code-unit check
// succeeds.
func (a *codeUnitAuthorizer) IsAuthorizedForWrite(requestPermission bool, requestReason string, toolName string, absPath ...string) error {
	if err := a.ensurePathsIncluded(absPath...); err != nil {
		return err
	}
	return a.fallback.IsAuthorizedForWrite(requestPermission, requestReason, toolName, absPath...)
}

// IsShellAuthorized delegates shell authorization to the fallback authorizer without applying code-unit path checks.
//
// Shell command paths are not modeled precisely enough for code-unit filtering.
func (a *codeUnitAuthorizer) IsShellAuthorized(requestPermission bool, requestReason string, cwd string, command []string) error {
	return a.fallback.IsShellAuthorized(requestPermission, requestReason, cwd, command)
}

// Close delegates cleanup to the fallback authorizer.
func (a *codeUnitAuthorizer) Close() {
	a.fallback.Close()
}

const codeUnitOutsideToolsHint = "Consider using other tools (ex: get_public_api; clarify_public_api; get_usage; update_usage; change_api)"

// codeUnitPathOutsideError reports that a requested path is outside the active code unit.
type codeUnitPathOutsideError struct {
	path     string // path is the requested path that was outside the code unit.
	unitName string // unitName is the code unit name used in the error message.
	unitDir  string // unitDir is the code unit base directory used in the error message.
}

// Error returns the denial message for a path outside the active code unit, including the unit name, root directory, and tool-use hint.
func (e codeUnitPathOutsideError) Error() string {
	return fmt.Sprintf("path %q is outside %q rooted at %q. %s", e.path, e.unitName, e.unitDir, codeUnitOutsideToolsHint)
}

// Is reports whether target is ErrCodeUnitPathOutside for errors.Is matching.
func (e codeUnitPathOutsideError) Is(target error) bool {
	return target == ErrCodeUnitPathOutside
}

// newCodeUnitPathOutsideError returns an error describing path as outside the active code unit.
//
// The error includes the code unit name and base directory for display to the caller.
func (a *codeUnitAuthorizer) newCodeUnitPathOutsideError(path string) error {
	return codeUnitPathOutsideError{
		path:     path,
		unitName: a.unit.Name(),
		unitDir:  a.unit.BaseDir(),
	}
}

// ensurePathsIncluded returns nil if every path is included in the active code unit.
//
// It returns a codeUnitPathOutsideError for the first path outside the unit. Relative rejected paths are reported from the code unit base directory.
func (a *codeUnitAuthorizer) ensurePathsIncluded(paths ...string) error {
	for _, path := range paths {
		if a.unit.Includes(path) {
			continue
		}
		canonical := path
		if !filepath.IsAbs(path) {
			canonical = filepath.Join(a.unit.BaseDir(), path)
		}
		return a.newCodeUnitPathOutsideError(canonical)
	}
	return nil
}

// ensurePathsIncludedOrGranted returns nil if every path is included in the code unit or has a read grant for toolName.
//
// Rejected paths are canonicalized for grant matching and error reporting. The method does not prompt the user; it returns a codeUnitPathOutsideError for the first
// path that is neither included nor granted.
func (a *codeUnitAuthorizer) ensurePathsIncludedOrGranted(toolName string, paths ...string) error {
	for _, path := range paths {
		if a.unit.Includes(path) {
			continue
		}

		canonical := path
		if !filepath.IsAbs(path) {
			canonical = filepath.Join(a.unit.BaseDir(), path)
		}
		canonical = filepath.Clean(canonical)

		if a.grants != nil && a.grants.isGrantedForRead(a.SandboxDir(), canonical, toolName, true) {
			continue
		}

		return a.newCodeUnitPathOutsideError(canonical)
	}
	return nil
}

// The baseAuthorizer type contains shared state for authorizers that manage user approval requests, read grants, and shutdown.
type baseAuthorizer struct {
	requests  chan UserRequest             // Requests carries pending user approval prompts and is closed by Close.
	grants    *grantStore                  // Grants stores user-message read grants that are evaluated during authorization.
	mu        sync.Mutex                   // Mu protects isClosed and pending.
	isClosed  bool                         // IsClosed records that Close has started and new authorization work must fail.
	pending   map[*pendingRequest]struct{} // Pending tracks unresolved approval requests so Close can unblock them.
	wg        sync.WaitGroup               // Wg waits for in-flight request enqueues before requests is closed.
	closedCh  chan struct{}                // ClosedCh is closed by Close to wake enqueues waiting to send on requests.
	closeOnce sync.Once                    // CloseOnce makes Close idempotent.
}

func newBaseAuthorizer() *baseAuthorizer {
	return &baseAuthorizer{
		requests: make(chan UserRequest, userRequestBufferSize),
		grants:   newGrantStore(),
		pending:  make(map[*pendingRequest]struct{}),
		closedCh: make(chan struct{}),
	}
}

// addGrantUserMessage records a user grant message for the base authorizer when grant storage is available.
func (b *baseAuthorizer) addGrantUserMessage(userMessage string) {
	if b.grants == nil {
		return
	}
	b.grants.addGrantUserMessage(userMessage)
}

// The checkOpen method reports whether the authorizer is still accepting work. It returns ErrAuthorizerClosed after Close has started.
func (b *baseAuthorizer) checkOpen() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.isClosed {
		return ErrAuthorizerClosed
	}
	return nil
}

// The promptForPaths method asks the user to approve a filesystem operation for paths. It returns nil immediately when paths is empty. Otherwise it builds a prompt
// from toolName, operation, scope, paths, requestReason, and requestPermission, then returns the result of the approval request; sandbox is accepted for call-site
// consistency and is not used.
func (b *baseAuthorizer) promptForPaths(toolName string, operation string, scope pathScope, sandbox string, paths []string, requestReason string, requestPermission bool) error {
	if len(paths) == 0 {
		return nil
	}

	_ = sandbox // keep signature consistent with historical callers

	prompt := buildPathPrompt(toolName, operation, scope, requestReason, paths, requestPermission)
	return b.requestApproval(prompt, toolName, nil)
}

// The promptForCommand method requests user approval for a shell command.
//
// The prompt includes the command classification, explicit permission request, outside-sandbox working-directory status, and request reason. The request carries
// command in UserRequest.Argv and returns nil only when approved.
func (b *baseAuthorizer) promptForCommand(cwd string, command []string, requestReason string, result CommandCheckResult, requestPermission bool, cwdOutside bool) error {
	commandString := strings.Join(command, " ")
	prompt := buildCommandPrompt(commandString, requestReason, result, requestPermission, cwdOutside)
	return b.requestApproval(prompt, "", command)
}

// The requestApproval method queues a UserRequest and waits for the user's decision. It returns nil when the request is allowed, ErrAuthorizationDenied when it
// is denied, and ErrAuthorizerClosed if the authorizer closes before the request completes. argv is copied into UserRequest.Argv when provided.
func (b *baseAuthorizer) requestApproval(prompt string, toolName string, argv []string) error {
	if err := b.checkOpen(); err != nil {
		return err
	}

	pending := newPendingRequest()
	req := UserRequest{
		ToolName: toolName,
		Prompt:   prompt,
	}
	if len(argv) > 0 {
		req.Argv = append([]string(nil), argv...)
	}
	req.Allow = func() {
		b.resolvePending(pending, decisionAllow)
	}
	req.Disallow = func() {
		b.resolvePending(pending, decisionDeny)
	}

	if err := b.enqueueRequest(req, pending); err != nil {
		return err
	}

	switch pending.wait() {
	case decisionAllow:
		return nil
	case decisionDeny:
		return ErrAuthorizationDenied
	case decisionClosed:
		return ErrAuthorizerClosed
	default:
		return ErrAuthorizationDenied
	}
}

// The enqueueRequest method records pending as active and delivers req to the user-request channel.
//
// It may block until the request is sent or Close interrupts the send. If the authorizer is already closed or Close prevents delivery, enqueueRequest removes the
// pending request and returns ErrAuthorizerClosed. It does not wait for the user's decision.
func (b *baseAuthorizer) enqueueRequest(req UserRequest, pending *pendingRequest) error {
	b.mu.Lock()
	if b.isClosed {
		b.mu.Unlock()
		return ErrAuthorizerClosed
	}
	b.pending[pending] = struct{}{}
	requestCh := b.requests
	closedCh := b.closedCh
	b.wg.Add(1)
	b.mu.Unlock()

	defer b.wg.Done()

	select {
	case requestCh <- req:
		return nil
	case <-closedCh:
		b.mu.Lock()
		delete(b.pending, pending)
		b.mu.Unlock()
		return ErrAuthorizerClosed
	}
}

// The resolvePending method records decision for a pending user request and wakes its waiter.
//
// It removes the request from the active pending set when the authorizer is still open. Repeated resolutions are safe because pendingRequest.finish is idempotent.
func (b *baseAuthorizer) resolvePending(pending *pendingRequest, decision authDecision) {
	b.mu.Lock()
	if !b.isClosed {
		delete(b.pending, pending)
	}
	b.mu.Unlock()
	pending.finish(decision)
}

// Close stops accepting authorization work and unblocks pending approval requests. It is idempotent. After Close starts, new authorization work fails with ErrAuthorizerClosed,
// the requests channel is closed after in-flight sends complete, and pending requests resolve as closed.
func (b *baseAuthorizer) Close() {
	b.closeOnce.Do(func() {
		b.mu.Lock()
		if b.isClosed {
			b.mu.Unlock()
			return
		}
		b.isClosed = true
		pending := make([]*pendingRequest, 0, len(b.pending))
		for p := range b.pending {
			pending = append(pending, p)
		}
		b.pending = make(map[*pendingRequest]struct{})
		close(b.closedCh)
		b.mu.Unlock()

		b.wg.Wait()
		close(b.requests)

		for _, p := range pending {
			p.finish(decisionClosed)
		}
	})
}

// authDecision is the internal result of a pending authorization request. It distinguishes approval, denial, and authorizer closure.
type authDecision int

const (
	decisionAllow authDecision = iota
	decisionDeny
	decisionClosed
)

// pendingRequest tracks the decision for one queued user authorization request.
type pendingRequest struct {
	once     sync.Once         // Once ensures the decision is recorded at most once.
	decision chan authDecision // Decision receives the approval, denial, or close result.
}

func newPendingRequest() *pendingRequest {
	return &pendingRequest{
		decision: make(chan authDecision, 1),
	}
}

// finish records decision and wakes any waiter.
//
// Only the first call has an effect.
func (p *pendingRequest) finish(decision authDecision) {
	p.once.Do(func() {
		p.decision <- decision
	})
}

// The wait method blocks until Allow, Disallow, or Close resolves the request and returns the recorded decision.
func (p *pendingRequest) wait() authDecision {
	return <-p.decision
}

// pathScope describes where requested paths are relative to the sandbox for approval prompts.
type pathScope string

const (
	scopeInsideSandbox  pathScope = "inside-sandbox"
	scopeOutsideSandbox pathScope = "outside-sandbox"
)

// buildPathPrompt returns the user-facing approval prompt for a filesystem operation. The prompt names toolName when provided, summarizes paths by value or count,
// includes the sandbox scope, and appends requestPermission and reason context when present.
func buildPathPrompt(toolName string, operation string, scope pathScope, reason string, paths []string, requestPermission bool) string {
	var actor string
	if toolName != "" {
		actor = fmt.Sprintf("tool %q", toolName)
	} else {
		actor = "the tool"
	}

	var scopeText string
	switch scope {
	case scopeInsideSandbox:
		scopeText = "inside the sandbox"
	case scopeOutsideSandbox:
		scopeText = "outside the sandbox"
	default:
		scopeText = "in the requested location"
	}

	var builder strings.Builder
	builder.WriteString("Allow ")
	builder.WriteString(actor)
	builder.WriteString(" to ")
	builder.WriteString(operation)
	builder.WriteByte(' ')
	if len(paths) == 1 {
		builder.WriteString(paths[0])
		builder.WriteByte(' ')
		builder.WriteString(scopeText)
		builder.WriteByte('?')
	} else {
		builder.WriteString(strconv.Itoa(len(paths)))
		builder.WriteString(" paths ")
		builder.WriteString(scopeText)
		builder.WriteByte('?')
	}

	if requestPermission {
		builder.WriteString(" (explicit permission requested)")
	}
	if reason != "" {
		builder.WriteString(" Reason: ")
		builder.WriteString(reason)
	}

	return builder.String()
}

// The buildCommandPrompt function formats the user-facing approval prompt for a shell command.
//
// The prompt includes the command, any command classification, outside-sandbox working-directory context, explicit permission marker, and request reason.
func buildCommandPrompt(command string, reason string, result CommandCheckResult, requestPermission bool, cwdOutside bool) string {
	var builder strings.Builder
	builder.WriteString("Allow execution of `")
	builder.WriteString(command)

	if result != CommandCheckResultNone {
		classification := commandCheckResultString(result)
		builder.WriteString("` (flagged as ")
		builder.WriteString(classification)
		builder.WriteString(")")
	}

	if cwdOutside {
		builder.WriteString(" with cwd outside sandbox")
	}
	if requestPermission {
		builder.WriteString(" (explicit permission requested)")
	}
	builder.WriteByte('?')

	if reason != "" {
		builder.WriteString(" Reason: ")
		builder.WriteString(reason)
	}

	return builder.String()
}

func commandCheckResultString(result CommandCheckResult) string {
	switch result {
	case CommandCheckResultSafe:
		return "safe"
	case CommandCheckResultBlocked:
		return "blocked"
	case CommandCheckResultDangerous:
		return "dangerous"
	case CommandCheckResultInscrutable:
		return "inscrutable"
	case CommandCheckResultNone:
		return "none"
	default:
		return "unknown"
	}
}

func classifyPaths(sandbox string, paths []string) (inside []string, outside []string, err error) {
	for _, raw := range paths {
		clean, err := normalizeAbsolutePath(raw)
		if err != nil {
			return nil, nil, err
		}
		if withinSandbox(sandbox, clean) {
			inside = append(inside, clean)
		} else {
			outside = append(outside, clean)
		}
	}
	return inside, outside, nil
}

func normalizeSandboxDir(sandbox string) (string, error) {
	if sandbox == "" {
		return "", errors.New("sandbox directory is empty")
	}

	abs, err := filepath.Abs(sandbox)
	if err != nil {
		return "", fmt.Errorf("normalize sandbox dir: %w", err)
	}
	return filepath.Clean(abs), nil
}

func normalizeAbsolutePath(path string) (string, error) {
	if path == "" {
		return "", errors.New("path is empty")
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("normalize path %q: %w", path, err)
	}
	return filepath.Clean(abs), nil
}

func withinSandbox(sandbox string, path string) bool {
	if sandbox == "" {
		return false
	}

	rel, err := filepath.Rel(sandbox, path)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	if rel == ".." {
		return false
	}
	prefix := ".." + string(filepath.Separator)
	return !strings.HasPrefix(rel, prefix)
}
