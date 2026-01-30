package noninteractive

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/codalotl/codalotl/internal/agent"
	"github.com/codalotl/codalotl/internal/agentformatter"
	"github.com/codalotl/codalotl/internal/agentsmd"
	"github.com/codalotl/codalotl/internal/codeunit"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/initialcontext"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/codalotl/codalotl/internal/prompt"
	"github.com/codalotl/codalotl/internal/tools/authdomain"
	"github.com/codalotl/codalotl/internal/tools/toolsets"
	"golang.org/x/term"
)

const (
	defaultModelID = llmmodel.DefaultModel
)

var toolCallPrintDelay = 3 * time.Second

// IsPrinted returns true if err has already been printed to the screen.
func IsPrinted(err error) bool {
	var pe *printedError
	return errors.As(err, &pe)
}

type printedError struct {
	err error
}

func (p *printedError) Error() string {
	if p == nil || p.err == nil {
		return ""
	}
	return p.err.Error()
}

func (p *printedError) Unwrap() error {
	if p == nil {
		return nil
	}
	return p.err
}

type Options struct {
	CWD string // working directory / sandbox dir. If "", uses os.Getwd()

	// PackagePath sets package mode with the path to a package vs CWD. If "", does not use package mode.
	// PackagePath can be any filesystem path (ex: "."; "/foo/bar"; "foo/bar"; "./foo/bar"). It must be rooted inside of CWD.
	PackagePath string

	// ModelID selects the LLM model for this run. If empty, uses the existing default model behavior.
	ModelID llmmodel.ModelID

	// Answers 'Yes' to any permission check. If false, we answer 'No' to any permission check. The end-user is never asked.
	AutoYes bool

	// NoFormatting=true means any prints do NOT use colors or other ANSI control codes to format. Only outputs plain text.
	// Otherwise, we default to the color scheme of the terminal and print colorized/formatted text.
	NoFormatting bool

	// If Out != nil, any prints we do will use Out; otherwise will use Stdout.
	// If Exec encounters errors during its run (eg: cannot talk to LLM; cannot write file), we'd still just print to Out (instead of something like Stderr).
	Out io.Writer
}

func effectiveModelID(opts Options) llmmodel.ModelID {
	if strings.TrimSpace(string(opts.ModelID)) != "" {
		return llmmodel.ModelID(strings.TrimSpace(string(opts.ModelID)))
	}
	return defaultModelID
}

type lockedWriter struct {
	w  io.Writer
	mu sync.Mutex
}

func (lw *lockedWriter) Write(p []byte) (int, error) {
	if lw == nil || lw.w == nil {
		return len(p), nil
	}
	lw.mu.Lock()
	defer lw.mu.Unlock()
	return lw.w.Write(p)
}

type delayedToolCallPrinter struct {
	out   io.Writer
	delay time.Duration

	mu      sync.Mutex
	closed  bool
	pending map[string]*pendingToolCall
}

type pendingToolCall struct {
	line     string
	canceled bool
	timer    *time.Timer
}

func newDelayedToolCallPrinter(out io.Writer, delay time.Duration) *delayedToolCallPrinter {
	return &delayedToolCallPrinter{
		out:     out,
		delay:   delay,
		pending: make(map[string]*pendingToolCall),
	}
}

func (p *delayedToolCallPrinter) Schedule(callID string, line string) {
	if p == nil || p.out == nil {
		return
	}
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return
	}
	if strings.TrimSpace(line) == "" {
		return
	}

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}

	if existing := p.pending[callID]; existing != nil {
		existing.canceled = true
		if existing.timer != nil {
			existing.timer.Stop()
		}
		delete(p.pending, callID)
	}

	entry := &pendingToolCall{line: line}
	entry.timer = time.AfterFunc(p.delay, func() {
		p.fire(callID)
	})
	p.pending[callID] = entry
	p.mu.Unlock()
}

func (p *delayedToolCallPrinter) Cancel(callID string) {
	if p == nil {
		return
	}
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	entry := p.pending[callID]
	if entry == nil {
		return
	}
	entry.canceled = true
	if entry.timer != nil {
		entry.timer.Stop()
	}
	delete(p.pending, callID)
}

func (p *delayedToolCallPrinter) Close() {
	if p == nil {
		return
	}

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	for callID, entry := range p.pending {
		if entry != nil {
			entry.canceled = true
			if entry.timer != nil {
				entry.timer.Stop()
			}
		}
		delete(p.pending, callID)
	}
	p.mu.Unlock()
}

func (p *delayedToolCallPrinter) fire(callID string) {
	if p == nil || p.out == nil {
		return
	}

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	entry := p.pending[callID]
	if entry == nil || entry.canceled {
		p.mu.Unlock()
		return
	}
	delete(p.pending, callID)
	line := entry.line
	p.mu.Unlock()

	if !strings.HasSuffix(line, "\n") {
		line += "\n"
	}
	_, _ = io.WriteString(p.out, line)
}

// Exec runs the agent with prompt and opts. It prints messages, tool calls, and so on to the screen.
//
// If there's any validation error (anything before the agent actually starts), an error is returned and nothing is nothing is printed.
// If there's an unhandled error and the agent cannot complete its run (ex: cannot talk to LLM, even after retries), a message may be printed AND returned via err.
// Callers can use IsPrinted to determine if an error has already been printed.
// Finally, note that many "errors" happen in the course of typical agent runs. For instance, the agent will ask to read non-existant files; shell commands will fail; etc. These
// do not typically constitute errors worthy of being returned (instead, the LLM is just told a file doesn't exist).
func Exec(userPrompt string, opts Options) error {
	userPrompt = strings.TrimSpace(userPrompt)
	if userPrompt == "" {
		return fmt.Errorf("prompt is required")
	}

	out := opts.Out
	if out == nil {
		out = os.Stdout
	}
	rawOut := out
	out = &lockedWriter{w: out}

	sandboxDir, err := normalizeSandboxDir(opts.CWD)
	if err != nil {
		return err
	}

	pkgMode := strings.TrimSpace(opts.PackagePath) != ""
	pkgRelPath, pkgAbsPath, err := normalizePackagePath(opts.PackagePath, sandboxDir)
	if err != nil {
		return err
	}

	formatter := agentformatter.NewTUIFormatter(agentformatter.Config{
		PlainText: opts.NoFormatting,
	})
	terminalWidth := detectTerminalWidth(rawOut)

	sandboxAuthorizer, userRequests, err := authdomain.NewPermissiveSandboxAuthorizer(sandboxDir, nil)
	if err != nil {
		return err
	}
	authorizerForTools, err := buildAuthorizerForTools(pkgMode, pkgRelPath, pkgAbsPath, sandboxAuthorizer, userPrompt, authdomain.AddGrantsFromUserMessage)
	if err != nil {
		sandboxAuthorizer.Close()
		return err
	}
	defer authorizerForTools.Close()

	go autoRespondToUserRequests(userRequests, out, opts.AutoYes)

	toolsForAgent, systemPrompt, err := buildToolsetAndSystemPrompt(pkgMode, sandboxDir, pkgAbsPath, authorizerForTools)
	if err != nil {
		return err
	}

	agentInstance, err := agent.NewAgent(effectiveModelID(opts), strings.TrimSpace(systemPrompt), toolsForAgent)
	if err != nil {
		return fmt.Errorf("construct agent: %w", err)
	}

	envMsg := buildEnvironmentInfo(sandboxDir)
	if pkgMode {
		envMsg = buildPackageEnvironmentInfo(sandboxDir, pkgRelPath, pkgAbsPath)
	}
	if err := agentInstance.AddUserTurn(envMsg); err != nil {
		return fmt.Errorf("add environment info: %w", err)
	}

	// In generic mode we don't gather package initialcontext, so include AGENTS.md
	// context up front if present.
	if !pkgMode {
		if agentsMsg := readAgentsMDContextBestEffort(sandboxDir, sandboxDir); agentsMsg != "" {
			if err := agentInstance.AddUserTurn(agentsMsg); err != nil {
				return fmt.Errorf("add AGENTS.md context: %w", err)
			}
		}
	}

	if err := printUserPrompt(out, userPrompt); err != nil {
		return err
	}

	runCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	toolCallPrinter := newDelayedToolCallPrinter(out, toolCallPrintDelay)
	defer toolCallPrinter.Close()

	var terminalErr error
	completedAssistantTurnsByAgent := make(map[string][]llmstream.Turn)
	for ev := range agentInstance.SendUserMessage(runCtx, userPrompt) {
		switch ev.Type {
		case agent.EventTypeAssistantTurnComplete:
			// Capture all completed assistant turns (including subagents) so we can optionally
			// report "ideal caching" token usage across the entire run, without incorrectly
			// letting prompt caching "carry over" between separate agent sessions.
			if ev.Turn != nil {
				completedAssistantTurnsByAgent[ev.Agent.ID] = append(completedAssistantTurnsByAgent[ev.Agent.ID], *ev.Turn)
			}
			// Suppress verbose per-turn usage/debug output like:
			// "• Turn complete: finish=... input=... output=... reasoning=... cached_input=..."
			//
			// We still print the user-visible completion line (EventTypeDoneSuccess) with
			// cumulative session token usage.
			continue
		case agent.EventTypeDoneSuccess:
			report := buildDoneSuccessReport(agentInstance.Turns(), completedAssistantTurnsByAgent, agentInstance.TokenUsage(), reportIdealCachingEnabled())

			if strings.TrimSpace(report.UsageAndCaching) != "" {
				s := report.UsageAndCaching
				if !strings.HasSuffix(s, "\n") {
					s += "\n"
				}
				if _, err := io.WriteString(out, s); err != nil {
					return err
				}
			}

			for _, line := range report.Lines {
				if strings.TrimSpace(line) == "" {
					continue
				}
				if !strings.HasSuffix(line, "\n") {
					line += "\n"
				}
				if _, err := io.WriteString(out, line); err != nil {
					return err
				}
			}
			continue
		case agent.EventTypeToolCall:
			formatted := formatter.FormatEvent(ev, terminalWidth)
			if shouldSuppressFormattedOutput(formatted) {
				continue
			}
			if formatted == "" {
				continue
			}

			callID := ""
			if ev.ToolCall != nil {
				callID = ev.ToolCall.CallID
			}

			// Delay printing the tool call itself: if the result comes back quickly,
			// only print the tool result. If it takes longer, print the call after a
			// short delay so a "hung" call is still visible in the log.
			if callID == "" || toolCallPrintDelay <= 0 {
				if !strings.HasSuffix(formatted, "\n") {
					formatted += "\n"
				}
				if _, err := io.WriteString(out, formatted); err != nil {
					return err
				}
			} else {
				toolCallPrinter.Schedule(callID, formatted)
			}
			continue
		case agent.EventTypeToolComplete:
			if ev.ToolResult != nil && strings.TrimSpace(ev.ToolResult.CallID) != "" {
				toolCallPrinter.Cancel(ev.ToolResult.CallID)
			}
			// Tool results are user-visible; print them like any other formatted event.
			formatted := formatter.FormatEvent(ev, terminalWidth)
			if shouldSuppressFormattedOutput(formatted) {
				continue
			}
			if formatted != "" {
				if !strings.HasSuffix(formatted, "\n") {
					formatted += "\n"
				}
				if _, err := io.WriteString(out, formatted); err != nil {
					return err
				}
			}
			continue
		default:
			formatted := formatter.FormatEvent(ev, terminalWidth)
			if shouldSuppressFormattedOutput(formatted) {
				continue
			}
			if formatted != "" {
				if !strings.HasSuffix(formatted, "\n") {
					formatted += "\n"
				}
				if _, err := io.WriteString(out, formatted); err != nil {
					return err
				}
			}
		}
		if ev.Type == agent.EventTypeError || ev.Type == agent.EventTypeCanceled {
			terminalErr = ev.Error
		}
	}

	if terminalErr != nil {
		return &printedError{err: terminalErr}
	}
	return nil
}

type grantsAdder func(authorizer authdomain.Authorizer, userMessage string) error

// turnSnapshotConversation adapts an agent's turn history to llmstream.StreamingConversation.
// It is intentionally read-only: it's only used for debugging/printing helpers.
type turnSnapshotConversation struct {
	turns []llmstream.Turn
}

var _ llmstream.StreamingConversation = (*turnSnapshotConversation)(nil)

func (c *turnSnapshotConversation) LastTurn() llmstream.Turn {
	if c == nil || len(c.turns) == 0 {
		return llmstream.Turn{}
	}
	return c.turns[len(c.turns)-1]
}

func (c *turnSnapshotConversation) Turns() []llmstream.Turn {
	if c == nil {
		return nil
	}
	return c.turns
}

func (c *turnSnapshotConversation) AddTools(_ []llmstream.Tool) error {
	return errors.New("turn snapshot conversation is read-only")
}

func (c *turnSnapshotConversation) AddUserTurn(_ string) error {
	return errors.New("turn snapshot conversation is read-only")
}

func (c *turnSnapshotConversation) AddToolResults(_ []llmstream.ToolResult) error {
	return errors.New("turn snapshot conversation is read-only")
}

func (c *turnSnapshotConversation) SendAsync(_ context.Context, _ ...llmstream.SendOptions) <-chan llmstream.Event {
	ch := make(chan llmstream.Event)
	close(ch)
	return ch
}

func buildAuthorizerForTools(pkgMode bool, pkgRelPath string, pkgAbsPath string, sandboxAuthorizer authdomain.Authorizer, userPrompt string, add grantsAdder) (authdomain.Authorizer, error) {
	authorizerForTools := sandboxAuthorizer
	if pkgMode {
		unitName := codeUnitName(pkgRelPath)
		unit, err := codeunit.NewCodeUnit(unitName, pkgAbsPath)
		if err != nil {
			return nil, fmt.Errorf("build code unit: %w", err)
		}
		// Expand beyond the base package dir to include supporting data directories while
		// excluding nested Go packages. This is snapshot-based: it is computed once at
		// startup.
		if err := unit.IncludeSubtreeUnlessContains("*.go"); err != nil {
			return nil, fmt.Errorf("expand code unit subtree: %w", err)
		}
		if err := includeReachableTestdataDirs(unit); err != nil {
			return nil, fmt.Errorf("include reachable testdata dirs: %w", err)
		}
		authorizerForTools = authdomain.NewCodeUnitAuthorizer(unit, sandboxAuthorizer)
	}

	// Apply grants to the active authorizer (including the code-unit wrapper), so that
	// `@...` paths in the user prompt are honored even in package mode.
	if err := applyGrantsFromUserPrompt(authorizerForTools, userPrompt, add); err != nil {
		return nil, err
	}

	return authorizerForTools, nil
}

// includeReachableTestdataDirs includes any "testdata" directory directly under an
// already-included directory (recursively). This allows Go fixture files in testdata
// to remain in-scope for a package-mode code unit even when they are "*.go" files.
func includeReachableTestdataDirs(unit *codeunit.CodeUnit) error {
	if unit == nil {
		return nil
	}

	// Iterate the current snapshot of included paths; this is intentionally not a
	// filesystem walk from BaseDir() because we only want testdata under directories
	// that are already reachable per the main code unit rules.
	for _, absPath := range unit.IncludedFiles() {
		info, err := os.Stat(absPath)
		if err != nil {
			// The code unit should only contain existing paths, but be tolerant.
			continue
		}
		if !info.IsDir() {
			continue
		}

		testdataPath := filepath.Join(absPath, "testdata")
		tdInfo, err := os.Stat(testdataPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("stat %q: %w", testdataPath, err)
		}
		if !tdInfo.IsDir() {
			continue
		}
		if unit.Includes(testdataPath) {
			continue
		}
		if err := unit.IncludeDir(testdataPath, true); err != nil {
			return fmt.Errorf("include %q: %w", testdataPath, err)
		}
	}

	return nil
}

func applyGrantsFromUserPrompt(authorizer authdomain.Authorizer, userPrompt string, add grantsAdder) error {
	if authorizer == nil || add == nil {
		return nil
	}
	if strings.TrimSpace(userPrompt) == "" {
		return nil
	}

	// Best-effort: if the current authorizer policy doesn't support grants, just ignore.
	if err := add(authorizer, userPrompt); err != nil {
		if errors.Is(err, authdomain.ErrAuthorizerCannotAcceptGrants) {
			return nil
		}
		return err
	}
	return nil
}

func shouldSuppressFormattedOutput(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	// Be defensive in case the formatter changes which event type emits this line.
	if strings.Contains(s, "Turn complete:") {
		return true
	}
	return false
}

func reportIdealCachingEnabled() bool {
	_, ok := os.LookupEnv("REPORT_IDEAL_CACHING")
	return ok
}

func providerAssistantTurns(turns []llmstream.Turn) []llmstream.Turn {
	out := make([]llmstream.Turn, 0, len(turns))
	for _, t := range turns {
		if t.Role != llmstream.RoleAssistant {
			continue
		}
		if t.ProviderID == "" {
			continue
		}
		out = append(out, t)
	}
	return out
}

// idealCachingForProviderTurns recalculates CachedInputTokens assuming each request's prompt
// is a prefix of the next request's prompt, and that the previous response's output is
// then included as input context for the next request.
//
// This models an "ideal" caching scenario where request N+1 can reuse the entire prompt
// from request N plus the response content from request N, so:
//
//	CachedInputTokens(N+1)=TotalInputTokens(N)+TotalOutputTokens(N)
//
// Values are clamped for safety when prompts shrink between requests.
//
// This was implemented because I was trying to measure token/cost performances in a benchmark scenario, and seemingly
// random cache misses in OpenAI were resulting in wildly different costs. For example, if one tool call result misses cache mid-conversation,
// we are billed the entire prefix conversation at 10x the cost (input vs cached input prices are are 10x different). I assume this happens because
// cache is best effort, caching servers can be created/destroyed due to scaling or natural cycling, cache entries evicted based on server load,
// and things of that nature. Example:
//
//	turn  provider_id                                              in_uncached  in_cached  out  cum_in_uncached  cum_in_cached  cum_out  cache
//	0     resp_00158c9a096bb94f00696a51f3d3c0819c89608cbc3d650b7c  2578         0          276  2578             0              276      n/a
//	1     resp_00158c9a096bb94f00696a51fc6424819c81172b97e208dfc1  253          2688       50   2831             2688           326      hit 91%
//	2     resp_00158c9a096bb94f00696a51ffa74c819c9737fb580126eec1  237          2816       57   3068             5504           383      hit 92%
//	3     resp_00158c9a096bb94f00696a52014304819c8a1633fffa3ce044  393          2944       225  3461             8448           608      hit 88%
//	4     resp_00158c9a096bb94f00696a520512f0819c970a4f932b257a28  143          3456       66   3604             11904          674      hit 96%
//	5     resp_00158c9a096bb94f00696a520b6b28819cb020374ad3ce4050  7789         0          107  11393            11904          781      miss 0% <- WHY?! Massive jump in uncached input tokens
//	6     resp_00158c9a096bb94f00696a5210d6ac819ca27291e4f259adcd  143          7808       146  11536            19712          927      hit 98%
func idealCachingForProviderTurns(turns []llmstream.Turn) ([]llmstream.Turn, llmstream.TokenUsage) {
	out := make([]llmstream.Turn, 0, len(turns))
	var session llmstream.TokenUsage

	var prevTotalInput int64
	var prevTotalOutput int64
	for i, t := range turns {
		totalIn := t.Usage.TotalInputTokens
		cached := int64(0)
		if i > 0 {
			cached = prevTotalInput + prevTotalOutput
			if cached > totalIn {
				cached = totalIn
			}
		}
		t.Usage.CachedInputTokens = cached
		out = append(out, t)

		prevTotalInput = totalIn
		prevTotalOutput = t.Usage.TotalOutputTokens

		session.TotalInputTokens += totalIn
		session.CachedInputTokens += cached
		session.ReasoningTokens += t.Usage.ReasoningTokens
		session.TotalOutputTokens += t.Usage.TotalOutputTokens
	}

	return out, session
}

type doneSuccessReport struct {
	UsageAndCaching string
	Lines           []string // lines are returned without trailing newlines
}

func buildDoneSuccessReport(actualTurns []llmstream.Turn, completedAssistantTurnsByAgent map[string][]llmstream.Turn, actualUsage llmstream.TokenUsage, reportIdealCaching bool) doneSuccessReport {
	// `UsageAndCaching` is a debug/trace output over the actual conversation and should not
	// change based on any "ideal caching" visualization.
	usageAndCaching := llmstream.UsageAndCaching(&turnSnapshotConversation{turns: actualTurns})

	if !reportIdealCaching {
		return doneSuccessReport{
			UsageAndCaching: usageAndCaching,
			Lines:           []string{formatAgentFinishedTurnLine(actualUsage)},
		}
	}

	idealUsage := idealCachingForCompletedTurnsByAgent(completedAssistantTurnsByAgent, actualTurns)
	return doneSuccessReport{
		UsageAndCaching: usageAndCaching,
		Lines: []string{
			formatActualTokenUsageLine(actualUsage),
			formatAgentFinishedTurnLine(idealUsage),
		},
	}
}

func idealCachingForCompletedTurnsByAgent(completedAssistantTurnsByAgent map[string][]llmstream.Turn, fallbackTurns []llmstream.Turn) llmstream.TokenUsage {
	turnsByAgent := completedAssistantTurnsByAgent
	if len(turnsByAgent) == 0 {
		turnsByAgent = map[string][]llmstream.Turn{"": fallbackTurns}
	}

	var session llmstream.TokenUsage
	for _, turns := range turnsByAgent {
		_, u := idealCachingForProviderTurns(providerAssistantTurns(turns))
		session.TotalInputTokens += u.TotalInputTokens
		session.CachedInputTokens += u.CachedInputTokens
		session.ReasoningTokens += u.ReasoningTokens
		session.TotalOutputTokens += u.TotalOutputTokens
	}
	return session
}

func formatActualTokenUsageLine(u llmstream.TokenUsage) string {
	// Keep the phrasing stable; callers/tests rely on this being a single line.
	return fmt.Sprintf("• actual token usage: %s", formatSessionTokenUsage(u))
}

func formatAgentFinishedTurnLine(u llmstream.TokenUsage) string {
	// Keep the phrasing stable; callers/tests rely on this being a single line.
	return fmt.Sprintf("• Agent finished the turn. Tokens: %s", formatSessionTokenUsage(u))
}

func formatSessionTokenUsage(u llmstream.TokenUsage) string {
	// Provider semantics vary about whether TotalInputTokens already includes CachedInputTokens.
	//
	// For CLI display, we want:
	// - input = non-cached input tokens
	// - cached_input = cached/reused input tokens
	//
	// We treat TotalInputTokens as "possibly inclusive" and subtract cached input,
	// clamping at 0 for safety.
	nonCachedInput := u.TotalInputTokens - u.CachedInputTokens
	if nonCachedInput < 0 {
		nonCachedInput = 0
	}
	total := nonCachedInput + u.CachedInputTokens + u.TotalOutputTokens
	return fmt.Sprintf("input=%d cached_input=%d output=%d total=%d", nonCachedInput, u.CachedInputTokens, u.TotalOutputTokens, total)
}

func autoRespondToUserRequests(requests <-chan authdomain.UserRequest, out io.Writer, autoYes bool) {
	for req := range requests {
		if out != nil && strings.TrimSpace(req.Prompt) != "" {
			decision := "NO"
			if autoYes {
				decision = "YES"
			}
			_, _ = fmt.Fprintf(out, "Permission: %s\nAuto decision: %s\n", req.Prompt, decision)
		}
		if autoYes {
			req.Allow()
		} else {
			req.Disallow()
		}
	}
}

func printUserPrompt(out io.Writer, prompt string) error {
	if out == nil {
		return nil
	}
	if strings.Contains(prompt, "\n") {
		_, err := fmt.Fprintf(out, "User:\n%s\n", prompt)
		return err
	}
	_, err := fmt.Fprintf(out, "> %s\n", prompt)
	return err
}

func normalizeSandboxDir(cwd string) (string, error) {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		cwd = wd
	}

	abs, err := filepath.Abs(cwd)
	if err != nil {
		return "", fmt.Errorf("resolve cwd %q: %w", cwd, err)
	}
	abs = filepath.Clean(abs)

	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("cwd %q does not exist", abs)
		}
		return "", fmt.Errorf("stat cwd %q: %w", abs, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("cwd %q is not a directory", abs)
	}
	return abs, nil
}

func normalizePackagePath(pkgPath string, sandboxDir string) (string, string, error) {
	pkgPath = strings.TrimSpace(pkgPath)
	if pkgPath == "" {
		return "", "", nil
	}

	sandboxDir = filepath.Clean(sandboxDir)
	if !filepath.IsAbs(sandboxDir) {
		return "", "", fmt.Errorf("cwd must be absolute")
	}

	normalized := pkgPath
	if filepath.IsAbs(normalized) {
		relToSandbox, err := filepath.Rel(sandboxDir, filepath.Clean(normalized))
		if err != nil {
			return "", "", fmt.Errorf("normalize package path: %w", err)
		}
		normalized = relToSandbox
	}

	if normalized == "" {
		normalized = "."
	}

	absPkgPath := filepath.Clean(filepath.Join(sandboxDir, normalized))
	relToSandbox, err := filepath.Rel(sandboxDir, absPkgPath)
	if err != nil {
		return "", "", fmt.Errorf("normalize package path: %w", err)
	}
	if relToSandbox == ".." || strings.HasPrefix(relToSandbox, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("package path %q is outside the sandbox %q", pkgPath, sandboxDir)
	}

	info, err := os.Stat(absPkgPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", fmt.Errorf("package path %q does not exist", pkgPath)
		}
		return "", "", fmt.Errorf("stat package path %q: %w", pkgPath, err)
	}
	if !info.IsDir() {
		return "", "", fmt.Errorf("package path %q is not a directory", pkgPath)
	}

	relToSandbox = filepath.ToSlash(relToSandbox)
	if relToSandbox == "" {
		relToSandbox = "."
	}

	return relToSandbox, absPkgPath, nil
}

func detectTerminalWidth(out io.Writer) int {
	if outFile, ok := out.(*os.File); ok && outFile != nil {
		fd := int(outFile.Fd())
		if term.IsTerminal(fd) {
			if w, _, err := term.GetSize(fd); err == nil && w > 0 {
				return w
			}
		}
	}
	if cols := strings.TrimSpace(os.Getenv("COLUMNS")); cols != "" {
		if n, err := strconv.Atoi(cols); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

func buildToolsetAndSystemPrompt(pkgMode bool, sandboxDir string, pkgAbsPath string, authorizer authdomain.Authorizer) ([]llmstream.Tool, string, error) {
	if pkgMode {
		systemPrompt := prompt.GetGoPackageModeModePrompt(prompt.GoPackageModePromptKindFull)
		tools, err := toolsets.PackageAgentTools(sandboxDir, authorizer, pkgAbsPath)
		if err != nil {
			return nil, "", fmt.Errorf("build package toolset: %w", err)
		}
		return tools, systemPrompt, nil
	}

	systemPrompt := prompt.GetFullPrompt()
	tools, err := toolsets.CoreAgentTools(sandboxDir, authorizer)
	if err != nil {
		return nil, "", fmt.Errorf("build toolset: %w", err)
	}
	return tools, systemPrompt, nil
}

func buildEnvironmentInfo(sandboxDir string) string {
	isGit := isGitRepo(sandboxDir)
	date := time.Now().Format("1/2/2006")
	return fmt.Sprintf(`Here is useful information about the environment you are running in:
<env>
Sandbox directory: %s
Is directory a git repo: %s
Platform: %s
Today's date: %s
</env>
`, sandboxDir, boolToYesNo(isGit), runtime.GOOS, date)
}

func isGitRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

func boolToYesNo(v bool) string {
	if v {
		return "Yes"
	}
	return "No"
}

func codeUnitName(pkgPath string) string {
	pkgPath = strings.TrimSpace(pkgPath)
	if pkgPath == "" || pkgPath == "." {
		return "package ."
	}
	return "package " + pkgPath
}

func buildPackageEnvironmentInfo(sandboxDir string, pkgRelPath string, pkgAbsPath string) string {
	baseInfo := buildEnvironmentInfo(sandboxDir)

	initialContext, err := buildPackageInitialContext(sandboxDir, pkgRelPath, pkgAbsPath)
	if err != nil {
		return baseInfo + "\n\n" + initialContext
	}

	return baseInfo + "\n" + initialContext
}

func loadGoPackage(pkgAbsPath string) (*gocode.Package, error) {
	if pkgAbsPath == "" {
		return nil, fmt.Errorf("empty package path")
	}
	module, err := gocode.NewModule(pkgAbsPath)
	if err != nil {
		return nil, fmt.Errorf("load module: %w", err)
	}

	relDir, err := filepath.Rel(module.AbsolutePath, pkgAbsPath)
	if err != nil {
		return nil, fmt.Errorf("resolve package dir: %w", err)
	}
	if relDir == "." {
		relDir = ""
	}

	pkg, err := module.LoadPackageByRelativeDir(relDir)
	if err != nil {
		return nil, fmt.Errorf("load package: %w", err)
	}

	return pkg, nil
}

func packagePathSection(pkgRelPath string, pkgAbsPath string, err error) string {
	var b strings.Builder
	b.WriteString("<package-mode>\n")
	fmt.Fprintf(&b, "Package relative path: %q\n", pkgRelPath)
	fmt.Fprintf(&b, "Package absolute path: %q\n", pkgAbsPath)
	if err != nil {
		fmt.Fprintf(&b, "Package details unavailable: %v\n", err)
	}
	b.WriteString("</package-mode>")
	return b.String()
}

func readAgentsMDContextBestEffort(sandboxDir, cwd string) string {
	msg, err := agentsmd.Read(sandboxDir, cwd)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(msg)
}

func buildPackageInitialContext(sandboxDir string, pkgRelPath string, pkgAbsPath string) (string, error) {
	agentsMsg := readAgentsMDContextBestEffort(sandboxDir, pkgAbsPath)

	pkg, err := loadGoPackage(pkgAbsPath)
	if err != nil {
		return joinContextBlocks(
			agentsMsg,
			packagePathSection(pkgRelPath, pkgAbsPath, err),
		), err
	}

	pkgModeInfo, err := initialcontext.Create(sandboxDir, pkg)
	if err != nil {
		return joinContextBlocks(
			agentsMsg,
			packagePathSection(pkgRelPath, pkgAbsPath, err),
		), err
	}

	// Always place AGENTS.md guidance before the rest of the generated initial context.
	return joinContextBlocks(agentsMsg, pkgModeInfo), nil
}

func joinContextBlocks(blocks ...string) string {
	nonEmpty := make([]string, 0, len(blocks))
	for _, b := range blocks {
		if strings.TrimSpace(b) == "" {
			continue
		}
		nonEmpty = append(nonEmpty, strings.TrimSpace(b))
	}
	return strings.Join(nonEmpty, "\n\n")
}
