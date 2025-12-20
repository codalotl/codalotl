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

const userRequestBufferSize = 16

// UserRequest describes work that requires a human decision.
type UserRequest struct {
	ToolCallID string
	ToolName   string
	Prompt     string
	Argv       []string
	Allow      func()
	Disallow   func()
}

// An Authorizer answers whether a tool is allowed to be used with respect to configured domains.
type Authorizer interface {
	// SandboxDir returns the cleaned, absolute path of the sandbox root for this authorizer.
	SandboxDir() string

	// CodeUnitDir returns the code unit base dir if a code unit domain is active, else "".
	CodeUnitDir() string

	// IsCodeUnitDomain reports whether this authorizer enforces a code unit domain.
	IsCodeUnitDomain() bool

	// WithoutCodeUnit returns an authorizer with code-unit restrictions removed.
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

// NewCodeUnitAuthorizer constructs an Authorizer that enforces membership in unit before delegating to fallback. Close also closes fallback.
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

type sandboxAuthorizer struct {
	*baseAuthorizer
	sandboxDir string
	commands   *ShellAllowedCommands
}

func (a *sandboxAuthorizer) SandboxDir() string {
	return a.sandboxDir
}

func (a *sandboxAuthorizer) CodeUnitDir() string {
	return ""
}

func (a *sandboxAuthorizer) IsCodeUnitDomain() bool {
	return false
}

func (a *sandboxAuthorizer) WithoutCodeUnit() Authorizer {
	return a
}

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

func (a *sandboxAuthorizer) Close() {
	a.baseAuthorizer.Close()
}

type permissiveSandboxAuthorizer struct {
	*baseAuthorizer
	sandboxDir string
	commands   *ShellAllowedCommands
}

func (a *permissiveSandboxAuthorizer) SandboxDir() string {
	return a.sandboxDir
}

func (a *permissiveSandboxAuthorizer) CodeUnitDir() string {
	return ""
}

func (a *permissiveSandboxAuthorizer) IsCodeUnitDomain() bool {
	return false
}

func (a *permissiveSandboxAuthorizer) WithoutCodeUnit() Authorizer {
	return a
}

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

func (a *permissiveSandboxAuthorizer) Close() {
	a.baseAuthorizer.Close()
}

type autoApproveAuthorizer struct {
	sandboxDir string
}

func (a autoApproveAuthorizer) SandboxDir() string {
	return a.sandboxDir
}

func (autoApproveAuthorizer) addGrantUserMessage(string) {}

func (autoApproveAuthorizer) CodeUnitDir() string {
	return ""
}

func (autoApproveAuthorizer) IsCodeUnitDomain() bool {
	return false
}

func (a autoApproveAuthorizer) WithoutCodeUnit() Authorizer {
	return a
}

func (autoApproveAuthorizer) IsAuthorizedForRead(bool, string, string, ...string) error {
	return nil
}

func (autoApproveAuthorizer) IsAuthorizedForWrite(bool, string, string, ...string) error {
	return nil
}

func (autoApproveAuthorizer) IsShellAuthorized(bool, string, string, []string) error {
	return nil
}

func (autoApproveAuthorizer) Close() {}

type codeUnitAuthorizer struct {
	unit     *codeunit.CodeUnit
	fallback Authorizer
	grants   *grantStore
}

func (a *codeUnitAuthorizer) addGrantUserMessage(userMessage string) {
	if a.grants == nil {
		return
	}
	a.grants.addGrantUserMessage(userMessage)
}

func (a *codeUnitAuthorizer) SandboxDir() string {
	return a.fallback.SandboxDir()
}

func (a *codeUnitAuthorizer) CodeUnitDir() string {
	return a.unit.BaseDir()
}

func (a *codeUnitAuthorizer) IsCodeUnitDomain() bool {
	return true
}

func (a *codeUnitAuthorizer) WithoutCodeUnit() Authorizer {
	return a.fallback
}

var codeUnitStrictReadToolNames = []string{"read_file", "ls", "diagnostics", "run_tests"}

func toolRequiresStrictReads(toolName string) bool {
	return slices.Contains(codeUnitStrictReadToolNames, toolName)
}

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

func (a *codeUnitAuthorizer) IsAuthorizedForWrite(requestPermission bool, requestReason string, toolName string, absPath ...string) error {
	if err := a.ensurePathsIncluded(absPath...); err != nil {
		return err
	}
	return a.fallback.IsAuthorizedForWrite(requestPermission, requestReason, toolName, absPath...)
}

func (a *codeUnitAuthorizer) IsShellAuthorized(requestPermission bool, requestReason string, cwd string, command []string) error {
	return a.fallback.IsShellAuthorized(requestPermission, requestReason, cwd, command)
}

func (a *codeUnitAuthorizer) Close() {
	a.fallback.Close()
}

func (a *codeUnitAuthorizer) ensurePathsIncluded(paths ...string) error {
	for _, path := range paths {
		if a.unit.Includes(path) {
			continue
		}
		canonical := path
		if !filepath.IsAbs(path) {
			canonical = filepath.Join(a.unit.BaseDir(), path)
		}
		return fmt.Errorf("path %q is outside %q rooted at %q. Consider using other tools (ex: 'get_public_api' provides docs for a package; 'clarify_public_api' can answer questions about poorly written docs)", canonical, a.unit.Name(), a.unit.BaseDir())
	}
	return nil
}

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

		return fmt.Errorf("path %q is outside %q rooted at %q. Consider using other tools (ex: 'get_public_api' provides docs for a package; 'clarify_public_api' can answer questions about poorly written docs)", canonical, a.unit.Name(), a.unit.BaseDir())
	}
	return nil
}

type baseAuthorizer struct {
	requests chan UserRequest
	grants   *grantStore

	mu       sync.Mutex
	isClosed bool
	pending  map[*pendingRequest]struct{}

	wg        sync.WaitGroup
	closedCh  chan struct{}
	closeOnce sync.Once
}

func newBaseAuthorizer() *baseAuthorizer {
	return &baseAuthorizer{
		requests: make(chan UserRequest, userRequestBufferSize),
		grants:   newGrantStore(),
		pending:  make(map[*pendingRequest]struct{}),
		closedCh: make(chan struct{}),
	}
}

func (b *baseAuthorizer) addGrantUserMessage(userMessage string) {
	if b.grants == nil {
		return
	}
	b.grants.addGrantUserMessage(userMessage)
}

func (b *baseAuthorizer) checkOpen() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.isClosed {
		return ErrAuthorizerClosed
	}
	return nil
}

func (b *baseAuthorizer) promptForPaths(toolName string, operation string, scope pathScope, sandbox string, paths []string, requestReason string, requestPermission bool) error {
	if len(paths) == 0 {
		return nil
	}

	_ = sandbox // keep signature consistent with historical callers

	prompt := buildPathPrompt(toolName, operation, scope, requestReason, paths, requestPermission)
	return b.requestApproval(prompt, toolName, nil)
}

func (b *baseAuthorizer) promptForCommand(cwd string, command []string, requestReason string, result CommandCheckResult, requestPermission bool, cwdOutside bool) error {
	commandString := strings.Join(command, " ")
	prompt := buildCommandPrompt(commandString, requestReason, result, requestPermission, cwdOutside)
	return b.requestApproval(prompt, "", command)
}

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

func (b *baseAuthorizer) resolvePending(pending *pendingRequest, decision authDecision) {
	b.mu.Lock()
	if !b.isClosed {
		delete(b.pending, pending)
	}
	b.mu.Unlock()
	pending.finish(decision)
}

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

type authDecision int

const (
	decisionAllow authDecision = iota
	decisionDeny
	decisionClosed
)

type pendingRequest struct {
	once     sync.Once
	decision chan authDecision
}

func newPendingRequest() *pendingRequest {
	return &pendingRequest{
		decision: make(chan authDecision, 1),
	}
}

func (p *pendingRequest) finish(decision authDecision) {
	p.once.Do(func() {
		p.decision <- decision
	})
}

func (p *pendingRequest) wait() authDecision {
	return <-p.decision
}

type pathScope string

const (
	scopeInsideSandbox  pathScope = "inside-sandbox"
	scopeOutsideSandbox pathScope = "outside-sandbox"
)

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
