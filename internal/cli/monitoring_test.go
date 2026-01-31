package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codalotl/codalotl/internal/q/remotemonitor"
	"github.com/stretchr/testify/require"
)

type testMonitorServer struct {
	latestVersion string
	latestDelay   time.Duration

	eventCount atomic.Int64
	lastEvent  atomic.Value // string

	errorCount   atomic.Int64
	lastErrorRaw atomic.Value // []byte
}

func newTestMonitorServer(t *testing.T) (*httptest.Server, *testMonitorServer) {
	t.Helper()

	s := &testMonitorServer{
		latestVersion: "0.0.0",
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/latest_version.json", func(w http.ResponseWriter, r *http.Request) {
		if s.latestDelay > 0 {
			time.Sleep(s.latestDelay)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"version":"`+s.latestVersion+`"}`)
	})
	mux.HandleFunc("/v1/reports/events", func(w http.ResponseWriter, r *http.Request) {
		s.eventCount.Add(1)
		ev := r.URL.Query().Get("event")
		s.lastEvent.Store(ev)
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/v1/reports/errors", func(w http.ResponseWriter, r *http.Request) {
		s.errorCount.Add(1)
		b, _ := io.ReadAll(r.Body)
		s.lastErrorRaw.Store(b)
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/v1/reports/panics", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	return httptest.NewServer(mux), s
}

func withTestMonitorFactory(t *testing.T, baseURL string) {
	t.Helper()
	orig := newCLIMonitor
	newCLIMonitor = func(currentVersion string) *remotemonitor.Monitor {
		m := remotemonitor.NewMonitor(currentVersion, baseURL)
		m.BuildToken = buildToken
		m.LatestVersionURL = baseURL + "/latest_version.json"
		m.ReportEventPath = "/v1/reports/events"
		m.ReportErrorPath = "/v1/reports/errors"
		m.ReportPanicPath = "/v1/reports/panics"
		return m
	}
	t.Cleanup(func() { newCLIMonitor = orig })
}

func TestNewCLIMonitor_SetsBuildToken(t *testing.T) {
	m := newCLIMonitor("1.2.3")
	require.NotNil(t, m)
	require.Equal(t, buildToken, m.BuildToken)
}

func TestRun_Version_UpdateAvailable_PrintsNoticeAndVersion(t *testing.T) {
	isolateUserConfig(t)

	srv, ms := newTestMonitorServer(t)
	t.Cleanup(srv.Close)
	withTestMonitorFactory(t, srv.URL)

	ms.latestVersion = "1.2.4"

	orig := Version
	Version = "1.2.3"
	t.Cleanup(func() { Version = orig })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "version"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())

	want := "An update is available: 1.2.4 (current 1.2.3)\nRun go install github.com/codalotl/codalotl@latest\n\n1.2.3\n"
	require.Equal(t, want, out.String())
}

func TestRun_Version_UpToDate_PrintsStatusAndVersion(t *testing.T) {
	isolateUserConfig(t)

	srv, ms := newTestMonitorServer(t)
	t.Cleanup(srv.Close)
	withTestMonitorFactory(t, srv.URL)

	ms.latestVersion = "1.2.3"

	orig := Version
	Version = "1.2.3"
	t.Cleanup(func() { Version = orig })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "version"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())

	want := "The current version (1.2.3) is up to date.\n\n1.2.3\n"
	require.Equal(t, want, out.String())
}

func TestRun_Version_LatestTimeout_PrintsOnlyVersion(t *testing.T) {
	isolateUserConfig(t)

	srv, ms := newTestMonitorServer(t)
	t.Cleanup(srv.Close)
	withTestMonitorFactory(t, srv.URL)

	ms.latestVersion = "9.9.9"
	ms.latestDelay = 500 * time.Millisecond

	orig := Version
	Version = "1.2.3"
	t.Cleanup(func() { Version = orig })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "version"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())

	require.Equal(t, "1.2.3\n", out.String())
}

func TestRun_Config_UpdateAvailable_PrintsNoticeFirst(t *testing.T) {
	isolateUserConfig(t)

	srv, ms := newTestMonitorServer(t)
	t.Cleanup(srv.Close)
	withTestMonitorFactory(t, srv.URL)

	ms.latestVersion = "1.2.4"

	orig := Version
	Version = "1.2.3"
	t.Cleanup(func() { Version = orig })

	tmp := t.TempDir()
	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "config"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())

	got := out.String()
	require.True(t, strings.HasPrefix(got, "An update is available: 1.2.4 (current 1.2.3)\nRun go install github.com/codalotl/codalotl@latest\n\n"))
	require.Greater(t, strings.Index(got, "Current Configuration:"), 0)
}

func TestRun_Config_DisableTelemetry_DoesNotSendEvent(t *testing.T) {
	isolateUserConfig(t)

	srv, ms := newTestMonitorServer(t)
	t.Cleanup(srv.Close)
	withTestMonitorFactory(t, srv.URL)

	ms.latestVersion = "1.2.4"

	orig := Version
	Version = "1.2.3"
	t.Cleanup(func() { Version = orig })

	tmp := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, ".codalotl"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, ".codalotl", "config.json"), []byte(`{"disabletelemetry":true}`+"\n"), 0644))

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "config"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())

	// Still show update notice (version check doesn't send data).
	require.True(t, strings.HasPrefix(out.String(), "An update is available:"))

	time.Sleep(150 * time.Millisecond)
	require.Equal(t, int64(0), ms.eventCount.Load())
}

func TestRun_Config_ExitCode1_ReportsErrorToMonitor(t *testing.T) {
	isolateUserConfig(t)

	srv, ms := newTestMonitorServer(t)
	t.Cleanup(srv.Close)
	withTestMonitorFactory(t, srv.URL)

	// Force startup validation to fail (exit code 1).
	t.Setenv("PATH", "")

	tmp := t.TempDir()
	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "config"}, &RunOptions{Out: &out, Err: &errOut})
	require.Error(t, err)
	require.Equal(t, 1, code)
	require.NotEmpty(t, errOut.String())

	require.Equal(t, int64(1), ms.errorCount.Load())
	rawAny := ms.lastErrorRaw.Load()
	require.NotNil(t, rawAny)

	raw := rawAny.([]byte)
	var obj map[string]any
	require.NoError(t, json.Unmarshal(raw, &obj))

	// The actual CLI error message can contain sensitive details; we send a
	// coarse category instead.
	require.Equal(t, "config_failed", obj["error"])
	meta := obj["metadata"].(map[string]any)
	require.Equal(t, "config", meta["event"])
	require.Equal(t, "1", meta["exit_code"])
	require.NotEmpty(t, meta["error_hash"])
}

func TestIsUpdateAvailable_ParsesSemverWithCommonPrefixes(t *testing.T) {
	require.True(t, isUpdateAvailable("v1.2.3", "1.2.4"))
	require.True(t, isUpdateAvailable("  V1.2.3  ", " v1.2.4 "))
	require.True(t, isUpdateAvailable("1.2", "1.2.1"))
}

func TestIsUpdateAvailable_RespectsSemverPrecedence(t *testing.T) {
	// Releases are higher precedence than pre-releases.
	require.True(t, isUpdateAvailable("1.2.3-rc1", "1.2.3"))

	// Pre-release identifiers compare per SemVer precedence rules.
	require.True(t, isUpdateAvailable("1.2.3-alpha", "1.2.3-rc1"))

	// Build metadata does not affect precedence.
	require.False(t, isUpdateAvailable("1.2.3+build.1", "1.2.3+build.2"))
}

func TestIsUpdateAvailable_InvalidInputs_ReturnFalse(t *testing.T) {
	require.False(t, isUpdateAvailable("", "1.2.3"))
	require.False(t, isUpdateAvailable("1.2.3", ""))
	require.False(t, isUpdateAvailable("dev", "1.2.3"))
	require.False(t, isUpdateAvailable("1.2.3", "dev"))
}
