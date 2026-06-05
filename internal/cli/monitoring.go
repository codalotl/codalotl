package cli

import (
	"errors"
	"fmt"
	"io"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/codalotl/codalotl/internal/q/remotemonitor"
	qsemver "github.com/codalotl/codalotl/internal/q/semver"
)

const (
	defaultMonitorHost       = "https://codalotl.ai"
	defaultLatestVersionURL  = "https://codalotl.github.io/codalotl/latest_version.json"
	defaultReportEventPath   = "/v1/reports/events"
	defaultReportErrorPath   = "/v1/reports/errors"
	defaultReportPanicPath   = "/v1/reports/panics"
	defaultVersionTimeout    = 250 * time.Millisecond
	defaultNoticeWaitTimeout = 150 * time.Millisecond
)

// buildToken is a "secret" token. It is sent as a header during communication with server. It is designed to prevent random abuse (ex: crawling bots hitting these
// endpoints), and is not designed to prevent targeted, deliberate abuse.
//
// For now, it's just hard-coded. In the future, it could be built into the build system with -ldflags. This is probably worthy of a redesign at some point.
const buildToken = "b80b45ed-c550-4fee-b088-da0eac4721f2"

// newCLIMonitor is a test hook. Production code should treat this like a constructor and not mutate it.
var newCLIMonitor = func(currentVersion string) *remotemonitor.Monitor {
	m := remotemonitor.NewMonitor(currentVersion, defaultMonitorHost)
	m.BuildToken = buildToken
	m.LatestVersionURL = defaultLatestVersionURL
	m.ReportEventPath = defaultReportEventPath
	m.ReportErrorPath = defaultReportErrorPath
	m.ReportPanicPath = defaultReportPanicPath

	// Stable, non-sensitive properties to help segment reports.
	m.SetStableProperties(map[string]string{
		"app": "codalotl",
	})
	return m
}

// cliRunState tracks monitoring and panic-reporting state for a CLI run.
type cliRunState struct {
	mu       sync.Mutex             // Protects monitor, event, and panicked.
	monitor  *remotemonitor.Monitor // Monitor used for telemetry, version checks, and panic reporting; nil when unavailable or disabled.
	event    string                 // Telemetry event name for the command currently running.
	panicked bool                   // panicked indicates the command panicked (regardless of whether crash reporting was enabled).
}

// SetEvent records the telemetry event name for the command currently running.
func (s *cliRunState) setEvent(event string) {
	s.mu.Lock()
	s.event = event
	s.mu.Unlock()
}

// Returns the telemetry event name associated with the current CLI command.
func (s *cliRunState) getEvent() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.event
}

// SetPanicked records that the current CLI run recovered from a panic.
func (s *cliRunState) setPanicked() {
	s.mu.Lock()
	s.panicked = true
	s.mu.Unlock()
}

// Returns whether the current CLI run has recovered from a panic.
func (s *cliRunState) getPanicked() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.panicked
}

// SetMonitor records m as the monitor for the current CLI run.
func (s *cliRunState) setMonitor(m *remotemonitor.Monitor) {
	s.mu.Lock()
	s.monitor = m
	s.mu.Unlock()
}

// Returns the monitor associated with the CLI run, if one has been initialized.
func (s *cliRunState) getMonitor() *remotemonitor.Monitor {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.monitor
}

func configureMonitorReporting(m *remotemonitor.Monitor, cfg Config) {
	if m == nil {
		return
	}
	// DisableTelemetry controls anonymous usage metrics and error reporting.
	// DisableCrashReporting controls panic reporting (crashes are panics only).
	m.SetReportingEnabled(!cfg.DisableCrashReporting, !cfg.DisableTelemetry, !cfg.DisableTelemetry)
}

func sanitizeStackForReporting(stack []byte) []byte {
	// runtime/debug stack traces frequently include absolute file paths (which
	// can include usernames). Keep only the base filename for .go frames.
	lines := strings.Split(string(stack), "\n")
	for i, line := range lines {
		goIdx := strings.Index(line, ".go:")
		if goIdx < 0 {
			continue
		}
		sep := strings.LastIndexAny(line[:goIdx], "/\\")
		if sep < 0 {
			continue
		}
		lines[i] = line[sep+1:]
	}
	return []byte(strings.Join(lines, "\n"))
}

// withPanicReporting runs f, reports any recovered panic when m is non-nil, and returns the panic as an error.
func withPanicReporting(m *remotemonitor.Monitor, state *cliRunState, event string, f func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			if state != nil {
				state.setPanicked()
			}

			// Avoid leaking local absolute paths (usernames) in stack traces.
			stack := sanitizeStackForReporting(debug.Stack())

			var reportErr error
			if m != nil {
				reportErr = m.ReportPanic(r, stack, map[string]string{"event": event})
			}
			if reportErr != nil {
				err = fmt.Errorf("panic: %v (also failed to report panic: %w)", r, reportErr)
				return
			}
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return f()
}

// versionStatusOutput returns user-facing version status text for currentVersion and latestVersion.
func versionStatusOutput(currentVersion, latestVersion string) (string, bool) {
	if strings.TrimSpace(currentVersion) == "" {
		return "", false
	}
	if strings.TrimSpace(latestVersion) == "" {
		return "", false
	}
	if strings.TrimSpace(currentVersion) == "dev" {
		return "", false
	}

	if isUpdateAvailable(currentVersion, latestVersion) {
		return fmt.Sprintf(
			"An update is available: %s (current %s)\nRun go install github.com/codalotl/codalotl@latest\n\n",
			latestVersion,
			currentVersion,
		), true
	}
	return fmt.Sprintf("The current version (%s) is up to date.\n\n", currentVersion), true
}

func maybeWriteUpdateNotice(w io.Writer, m *remotemonitor.Monitor, currentVersion string, wait time.Duration) error {
	latest, ok := latestVersionWithTimeout(m, wait)
	if !ok {
		return nil
	}
	notice, ok := versionStatusOutput(currentVersion, latest)
	if !ok {
		return nil
	}
	// For non-version commands, we only show notices when out of date.
	if !strings.HasPrefix(notice, "An update is available:") {
		return nil
	}
	_, err := io.WriteString(w, notice)
	return err
}

// latestVersionWithTimeout returns the monitor's latest known version before timeout.
func latestVersionWithTimeout(m *remotemonitor.Monitor, timeout time.Duration) (string, bool) {
	if m == nil {
		return "", false
	}

	type res struct {
		v   string
		err error
	}
	ch := make(chan res, 1)
	go func() {
		v, err := m.LatestVersionSync()
		ch <- res{v: v, err: err}
	}()

	select {
	case r := <-ch:
		if r.err != nil {
			return "", false
		}
		return r.v, true
	case <-time.After(timeout):
		return "", false
	}
}

func isUpdateAvailable(current, latest string) bool {
	c, err := qsemver.Parse(current)
	if err != nil {
		return false
	}
	l, err := qsemver.Parse(latest)
	if err != nil {
		return false
	}
	return qsemver.GreaterThan(l, c)
}

// reportErrorForExitCode1 reports an exit-code-1 command failure to the monitor.
func reportErrorForExitCode1(m *remotemonitor.Monitor, event string, msg string) error {
	if m == nil {
		return nil
	}
	if msg == "" {
		msg = "command failed"
	}

	meta := map[string]string{
		"event":     event,
		"exit_code": "1",
	}

	// If the event is empty, this report isn't very useful; keep it but label it.
	if strings.TrimSpace(event) == "" {
		meta["event"] = "unknown"
	}

	return m.ReportError(errors.New(msg), meta)
}
