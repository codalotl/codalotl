package remotemonitor

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMonitor(t *testing.T) {
	m := NewMonitor("dev", "https://h")
	assert.Equal(t, "", m.CurrentVersion)
	assert.Equal(t, "https://h", m.Host)
	// stable props should always include version
	cp := m.copyStableProps()
	require.NotNil(t, cp)
	assert.Equal(t, "", cp["version"]) // dev coerces to empty version

	m2 := NewMonitor("1.2.3", "h")
	assert.Equal(t, "1.2.3", m2.CurrentVersion)
	assert.Equal(t, "h", m2.Host)
	cp2 := m2.copyStableProps()
	require.NotNil(t, cp2)
	assert.Equal(t, "1.2.3", cp2["version"]) // version included in stable props
}

func TestSetStableProperties_MergeAndNil(t *testing.T) {
	var nilMon *Monitor
	// Should not panic on nil receiver or nil props
	nilMon.SetStableProperties(nil)

	m := &Monitor{}
	m.SetStableProperties(map[string]string{"a": "1"})
	m.SetStableProperties(map[string]string{"b": "2", "a": "3"})

	cp := m.copyStableProps()
	require.NotNil(t, cp)
	assert.Equal(t, "3", cp["a"]) // overwritten
	assert.Equal(t, "2", cp["b"]) // merged
}

func TestBuildURL(t *testing.T) {
	m := &Monitor{Host: "https://host/"}

	// Absolute pass-through
	if u, ok := m.buildURL("http://x/y", "/d"); assert.True(t, ok) {
		assert.Equal(t, "http://x/y", u)
	}

	// Default path used
	if u, ok := m.buildURL("", "/v"); assert.True(t, ok) {
		assert.Equal(t, "https://host/v", u)
	}

	// Path joins with single slash
	m.Host = "https://host"
	if u, ok := m.buildURL("/v", ""); assert.True(t, ok) {
		assert.Equal(t, "https://host/v", u)
	}

	// Invalid when host empty and non-absolute path
	m.Host = ""
	_, ok := m.buildURL("/v", "")
	assert.False(t, ok)
}

func TestSafeCopyMap(t *testing.T) {
	assert.Nil(t, safeCopyMap(nil))
	assert.Nil(t, safeCopyMap(map[string]string{}))

	src := map[string]string{"a": "1"}
	cp := safeCopyMap(src)
	require.NotNil(t, cp)
	assert.Equal(t, src, cp)
	// Mutation does not affect original
	cp["a"] = "2"
	assert.Equal(t, "1", src["a"])
}

func TestLatestVersionSync_SuccessAndCache(t *testing.T) {
	var hits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			// Assert Build-Token header not required by default (empty)
			if r.Header.Get("Build-Token") != "" {
				t.Errorf("unexpected Build-Token header: %q", r.Header.Get("Build-Token"))
			}
			atomic.AddInt32(&hits, 1)
			w.WriteHeader(http.StatusOK)
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"version":"1.2.3"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	m := NewMonitor("", "")
	m.LatestVersionURL = server.URL + "/version"

	// First call fetches
	v, err := m.LatestVersionSync()
	require.NoError(t, err)
	assert.Equal(t, "1.2.3", v)

	// Second call uses cache
	v2, err2 := m.LatestVersionSync()
	require.NoError(t, err2)
	assert.Equal(t, "1.2.3", v2)
	assert.Equal(t, int32(1), atomic.LoadInt32(&hits))
}

func TestLatestVersionSync_ServerErrorAndAsyncRead(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// If Build-Token is set, ensure header is present
		if strings.Contains(r.URL.Path, "/version_with_token") {
			if r.Header.Get("Build-Token") != "token123" {
				t.Errorf("expected Build-Token header 'token123', got %q", r.Header.Get("Build-Token"))
			}
		}
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"oops"}`))
	}))
	defer server.Close()

	m := NewMonitor("", "")
	m.LatestVersionURL = server.URL + "/version_with_token"
	m.BuildToken = "token123"

	// Populate cache with error
	v, err := m.LatestVersionSync()
	assert.Empty(t, v)
	require.Error(t, err)

	// Async returns cached error
	_, err2 := m.LatestVersionAsync()
	require.Error(t, err2)
}

func TestLatestVersionSync_InvalidHostOrURL(t *testing.T) {
	m := NewMonitor("", "")
	// LatestVersionURL empty and Host empty -> invalid
	_, err := m.LatestVersionSync()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid host or LatestVersionURL")
}

func TestFetchLatestVersionFromHost_NonBlockingAndSingleFlight(t *testing.T) {
	var hits int32
	done := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			// Build-Token header honored when set
			if r.Header.Get("Build-Token") != "v2token" {
				t.Errorf("expected Build-Token header 'v2token', got %q", r.Header.Get("Build-Token"))
			}
			atomic.AddInt32(&hits, 1)
			w.WriteHeader(http.StatusOK)
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"version":"2.0.0"}`))
			close(done)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	m := NewMonitor("", "")
	m.LatestVersionURL = server.URL + "/version"
	m.BuildToken = "v2token"

	// Trigger twice; should only perform one network call
	m.FetchLatestVersionFromHost()
	m.FetchLatestVersionFromHost()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for async fetch")
	}

	// Eventually cache should be populated
	deadline := time.Now().Add(2 * time.Second)
	for {
		v, err := m.LatestVersionAsync()
		if v != "" || err != ErrNotCached {
			assert.Equal(t, "2.0.0", v)
			assert.NoError(t, err)
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("cache not populated in time")
		}
		time.Sleep(10 * time.Millisecond)
	}
	assert.Equal(t, int32(1), atomic.LoadInt32(&hits))
}

func TestReportError_SuccessAndFailures(t *testing.T) {
	recv := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/report_error" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// Header must be present when set
		if r.Header.Get("Build-Token") != "errtok" {
			t.Errorf("expected Build-Token header 'errtok', got %q", r.Header.Get("Build-Token"))
		}
		defer r.Body.Close()
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		recv <- payload
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	m := NewMonitor("", server.URL)
	m.ReportErrorPath = "/report_error"
	m.BuildToken = "errtok"
	m.SetStableProperties(map[string]string{"stable": "yes"})

	// Nil error should be no-op
	require.NoError(t, m.ReportError(nil, map[string]string{"k": "v"}))

	// Success
	require.NoError(t, m.ReportError(assert.AnError, map[string]string{"k": "v"}))

	select {
	case got := <-recv:
		// Basic fields
		assert.Equal(t, assert.AnError.Error(), got["error"]) // string
		// metadata
		md, _ := got["metadata"].(map[string]any)
		if assert.NotNil(t, md) {
			assert.Equal(t, "v", md["k"])
		}
		// props
		props, _ := got["props"].(map[string]any)
		if assert.NotNil(t, props) {
			assert.Equal(t, "yes", props["stable"])
		}
		// host properties (spot-check a few keys)
		hostProps, _ := got["host"].(map[string]any)
		if assert.NotNil(t, hostProps) {
			assert.NotEmpty(t, hostProps["goos"])
			assert.NotEmpty(t, hostProps["goarch"])
		}
	case <-time.After(time.Second):
		t.Fatalf("did not receive report payload")
	}

	// Empty path is no-op
	m.ReportErrorPath = ""
	require.NoError(t, m.ReportError(assert.AnError, nil))

	// Invalid host or URL
	m.ReportErrorPath = "/report_error"
	m.Host = ""
	require.Error(t, m.ReportError(assert.AnError, nil))

	// Non-2xx
	server500 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server500.Close()
	m.Host = server500.URL
	require.Error(t, m.ReportError(assert.AnError, nil))
}

func TestReportPanic_SuccessAndFailures(t *testing.T) {
	recv := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/report_panic" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// Header must be present when set
		if r.Header.Get("Build-Token") != "pantok" {
			t.Errorf("expected Build-Token header 'pantok', got %q", r.Header.Get("Build-Token"))
		}
		defer r.Body.Close()
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		recv <- payload
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	m := NewMonitor("", server.URL)
	m.ReportPanicPath = "/report_panic"
	m.BuildToken = "pantok"
	m.SetStableProperties(map[string]string{"stable": "yes"})

	stack := []byte("stacktrace here")
	require.NoError(t, m.ReportPanic("boom", stack, map[string]string{"k": "v"}))

	select {
	case got := <-recv:
		assert.Equal(t, "boom", got["panic"])            // stringified
		assert.Equal(t, "stacktrace here", got["stack"]) // string
		md, _ := got["metadata"].(map[string]any)
		if assert.NotNil(t, md) {
			assert.Equal(t, "v", md["k"])
		}
	case <-time.After(time.Second):
		t.Fatalf("did not receive panic payload")
	}

	// Empty path: no-op
	m.ReportPanicPath = ""
	require.NoError(t, m.ReportPanic("x", nil, nil))

	// Invalid host or URL
	m.ReportPanicPath = "/report_panic"
	m.Host = ""
	require.Error(t, m.ReportPanic("x", nil, nil))

	// Non-2xx
	server500 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server500.Close()
	m.Host = server500.URL
	require.Error(t, m.ReportPanic("x", nil, nil))
}

func TestReportEventAsync_QueryAndProps(t *testing.T) {
	recv := make(chan url.Values, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/report_event" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("Build-Token") != "evtok" {
			t.Errorf("expected Build-Token header 'evtok', got %q", r.Header.Get("Build-Token"))
		}
		recv <- r.URL.Query()
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer server.Close()

	m := NewMonitor("", server.URL)
	m.ReportEventPath = "/report_event"
	m.BuildToken = "evtok"
	m.SetStableProperties(map[string]string{"stable": "yes"})

	// Without stable props
	m.ReportEventAsync("evt", map[string]string{"a": "1", "": "skip"}, false)
	select {
	case q := <-recv:
		assert.Equal(t, "evt", q.Get("e"))
		assert.Equal(t, "1", q.Get("a"))
		_, hasEmpty := q[""]
		assert.False(t, hasEmpty)
		assert.Empty(t, q.Get("stable"))
	case <-time.After(time.Second):
		t.Fatalf("no event received (without props)")
	}

	// With stable props
	m.ReportEventAsync("evt2", map[string]string{"b": "2"}, true)
	select {
	case q := <-recv:
		assert.Equal(t, "evt2", q.Get("e"))
		assert.Equal(t, "2", q.Get("b"))
		assert.Equal(t, "yes", q.Get("stable"))
	case <-time.After(time.Second):
		t.Fatalf("no event received (with props)")
	}
}

func TestSetReportingEnabled_DisableAll_NoNetwork(t *testing.T) {
	var called int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&called, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	m := NewMonitor("", server.URL)
	m.ReportErrorPath = "/e"
	m.ReportPanicPath = "/p"
	m.ReportEventPath = "/ev"
	m.SetReportingEnabled(false, false, false)

	// All should be no-ops
	_ = m.ReportError(assert.AnError, nil)
	_ = m.ReportPanic("x", nil, nil)
	m.ReportEventAsync("evt", nil, false)

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int32(0), atomic.LoadInt32(&called))
}

func TestSetReportingEnabled_Selective(t *testing.T) {
	var p, e, v int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/p":
			atomic.AddInt32(&p, 1)
		case "/e":
			if r.Method == http.MethodPost {
				atomic.AddInt32(&e, 1)
			}
		case "/v":
			if r.Method == http.MethodGet {
				atomic.AddInt32(&v, 1)
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	m := NewMonitor("", server.URL)
	m.ReportPanicPath = "/p"
	m.ReportErrorPath = "/e"
	m.ReportEventPath = "/v"

	// Only enable errors
	m.SetReportingEnabled(false, true, false)
	_ = m.ReportError(assert.AnError, nil)
	_ = m.ReportPanic("boom", nil, nil)
	m.ReportEventAsync("evt", nil, false)
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int32(0), atomic.LoadInt32(&p))
	assert.Equal(t, int32(1), atomic.LoadInt32(&e))
	assert.Equal(t, int32(0), atomic.LoadInt32(&v))

	// Now only enable events
	m.SetReportingEnabled(false, false, true)
	_ = m.ReportError(assert.AnError, nil)
	_ = m.ReportPanic("boom", nil, nil)
	m.ReportEventAsync("evt2", nil, false)
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int32(0), atomic.LoadInt32(&p))
	assert.Equal(t, int32(1), atomic.LoadInt32(&e))
	assert.Equal(t, int32(1), atomic.LoadInt32(&v))
}

func TestWithPanicReporting_NoPanic(t *testing.T) {
	m := &Monitor{}
	called := false
	err := m.WithPanicReporting(func() {
		called = true
	})
	require.NoError(t, err)
	assert.True(t, called)
}

func TestWithPanicReporting_PanicReportsAndReturnsError(t *testing.T) {
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/panic" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		defer r.Body.Close()
		_ = json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	m := NewMonitor("", server.URL)
	m.ReportPanicPath = "/panic"

	err := m.WithPanicReporting(func() {
		panic("kaboom")
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "panic recovered: kaboom")

	// Ensure a report was sent with expected basic fields
	require.NotNil(t, received)
	assert.Equal(t, "kaboom", received["panic"])
	// stack should be present and non-empty string
	if s, _ := received["stack"].(string); assert.NotEmpty(t, s) {
		assert.Contains(t, s, "TestWithPanicReporting_PanicReportsAndReturnsError")
	}
}

func TestWithPanicReporting_ReportFailureIncludedInError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always fail
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	m := NewMonitor("", server.URL)
	m.ReportPanicPath = "/panic"

	err := m.WithPanicReporting(func() { panic("boom2") })
	require.Error(t, err)
	assert.Contains(t, err.Error(), "panic recovered: boom2")
	assert.Contains(t, err.Error(), "report error")
}

func TestHostProperties_Sanity(t *testing.T) {
	props := HostProperties()
	require.NotNil(t, props)
	// Spot-check a few keys
	assert.NotEmpty(t, props["goos"])
	assert.Equal(t, runtime.GOOS, props["goos"])
	assert.NotEmpty(t, props["goarch"])
	assert.Equal(t, runtime.GOARCH, props["goarch"])
	assert.NotEmpty(t, props["time"]) // RFC3339 string
	// Numeric fields parse
	if v := props["numcpu"]; v != "" {
		_, err := strconv.Atoi(v)
		assert.NoError(t, err)
	}
	if v := props["mem_alloc"]; v != "" {
		_, err := strconv.ParseUint(v, 10, 64)
		assert.NoError(t, err)
	}
	// container is normalized
	assert.Contains(t, []string{"true", "false"}, props["container"])
	// tz non-empty string (format varies by environment)
	assert.NotEqual(t, "", props["tz"])
}

func TestTwoDigit(t *testing.T) {
	assert.Equal(t, "00", twoDigit(0))
	assert.Equal(t, "09", twoDigit(9))
	assert.Equal(t, "10", twoDigit(10))
}

func TestSplitKeyVal(t *testing.T) {
	key, val, ok := splitKeyVal("PRETTY_NAME=\"Ubuntu 22.04 LTS\"")
	assert.True(t, ok)
	assert.Equal(t, "PRETTY_NAME", key)
	assert.Equal(t, "Ubuntu 22.04 LTS", val)

	key, val, ok = splitKeyVal("NAME='Ubuntu' ")
	assert.True(t, ok)
	assert.Equal(t, "NAME", key)
	assert.Equal(t, "Ubuntu", val)

	_, _, ok = splitKeyVal("invalid line")
	assert.False(t, ok)
}

func TestExtractRegValue(t *testing.T) {
	// Typical lines from reg.exe
	assert.Equal(t, "Windows 11 Pro", strings.TrimSpace(extractRegValue("ProductName    REG_SZ    Windows 11 Pro")))
	assert.Equal(t, "24H2", strings.TrimSpace(extractRegValue("DisplayVersion REG_SZ 24H2")))
	assert.Equal(t, "", extractRegValue("NoType Here"))
}

func TestFileExists(t *testing.T) {
	tmp := t.TempDir()
	file := filepath.Join(tmp, "f.txt")
	// Initially false
	assert.False(t, fileExists(file))
	// Create file
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o644))
	assert.True(t, fileExists(file))
	// Directories are not files
	assert.False(t, fileExists(tmp))
}
