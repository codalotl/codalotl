package remotemonitor

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Monitor represents a remote monitor embedded in a CLI binary that is running on a customer's computer (could be server
// or laptop or desktop; personal or corporate; OS could be varied; network access varied).
//
// It is used to make sure the version is up to date, report anonymous usage activity, and report serious errors/panics,
// so the server can assess health of the fleet of binaries.
type Monitor struct {
	// Current version that is running.
	CurrentVersion string

	// Host is the server that this monitor reports to and queries. It must include scheme. Trailing slash optional.
	Host string

	// Caller-defined opaque bag of props that are included in ReportError/Panic. ReportEvent is optionally included.
	stableProps map[string]string

	BuildToken string // optional token. If set, includes Build-Token: xyz as an HTTP header for the requests.

	// URL to query (GET) for latest version. If just a path, combines with Host. It can also include a host, which possibly
	// differs from Host. If you want query params like ?product=x, you must bake that into LatestVersionURL.
	//
	// Defaults to "/version" if unset.
	//
	// Response format: JSON object {"version":"1.2.3"}.
	//
	// LatestVersionURL allows full URLs because this is the highest throughput endpoint, does not necessarily require server
	// computation to conditionally compute different versions for different people, and so can easily be hosted by an object
	// store or CDN instead of hitting host directly.
	LatestVersionURL string

	ReportErrorPath string // Ex: "/report_error". Will receive a POST when ReportError is called.
	ReportPanicPath string // Ex: "/report_panic". Will receive a POST when ReportPanic is called.
	ReportEventPath string // Ex: "/report_event". Will receive a GET when ReportError is called.

	// latestVersion is the cached latest-version string from the most recent successful check. Guarded by mu.
	latestVersion string

	// latestVersionErr is the cached error from the most recent latest-version check. Guarded by mu.
	latestVersionErr error

	// mu guards mutable state: stableProps, latestVersion, latestVersionErr, inflight, and httpClient.
	mu sync.Mutex

	// inflight is non-nil while a latest-version request is in progress. It is closed to signal completion to waiters.
	inflight chan struct{}

	// httpClient is the client used for all network operations. If nil, it is lazily initialized with a 4s timeout. Callers
	// may replace it before use to customize transport or timeouts; the replacement must be safe for concurrent use.
	httpClient *http.Client

	panicReportingDisabled bool
	errorReportingDisabled bool
	eventReportingDisabled bool
}

// NewMonitor creates a new monitor with current version and host. If version is "dev" or "", version is considered unset.
//
// Callers can set LatestVersionURL/etc after calling NewMonitor.
func NewMonitor(version, host string) *Monitor {
	if version == "dev" {
		version = ""
	}

	return &Monitor{
		CurrentVersion: version,
		Host:           host,
		stableProps:    map[string]string{"version": version},
	}
}

// SetReportingEnabled allows supression of (and then enabling of) reporting events to Host (everything is enabled by default). If reporting of a kind is disabled,
// calls to report of that kind are silently succeeded. This allows opt-in or opt-out of telemetry by a user, which configures a Monitor, but then have the monitor
// be just passed around and reported on like normal.
func (m *Monitor) SetReportingEnabled(panicReporting, errorReporting, eventReportig bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.panicReportingDisabled = !panicReporting
	m.errorReportingDisabled = !errorReporting
	m.eventReportingDisabled = !eventReportig
}

// SetStableProperties sets stable props that are included in ReportError and ReportPanic requests; ReportEvent is optionally
// included. Merges with existing props if they're present.
func (m *Monitor) SetStableProperties(props map[string]string) {
	if props == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stableProps == nil {
		m.stableProps = make(map[string]string, len(props))
	}
	maps.Copy(m.stableProps, props)
}

// LatestVersionSync returns the latest version from host, blocking until complete.
//   - Returns cached version or cached error if set.
//   - If a request is in flight via fetchLatestVersion, waits for that to finish.
//   - Otherwise, calls fetchLatestVersion and waits for it to finish.
func (m *Monitor) LatestVersionSync() (string, error) {
	// Fast path: return cached answer if present (either value or error).
	m.mu.Lock()
	if m.latestVersion != "" || m.latestVersionErr != nil {
		v, e := m.latestVersion, m.latestVersionErr
		m.mu.Unlock()
		return v, e
	}

	// If a request is already in flight, wait for it.
	if m.inflight != nil {
		ch := m.inflight
		m.mu.Unlock()
		<-ch
		m.mu.Lock()
		v, e := m.latestVersion, m.latestVersionErr
		m.mu.Unlock()
		return v, e
	}

	// Start a request and wait for it.
	ch := make(chan struct{})
	m.inflight = ch
	m.mu.Unlock()

	m.fetchLatestVersion()
	<-ch

	m.mu.Lock()
	v, e := m.latestVersion, m.latestVersionErr
	m.mu.Unlock()
	return v, e
}

// LatestVersionAsync returns cached version. If no version is cached, returns ("", ErrNotCached).
func (m *Monitor) LatestVersionAsync() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.latestVersion == "" && m.latestVersionErr == nil {
		return "", ErrNotCached
	}
	return m.latestVersion, m.latestVersionErr
}

// FetchLatestVersionFromHost initiates a request to the host to get the latest version unless the receiver is nil or a fetch
// is already in progress. It does not block; it returns immediately.
func (m *Monitor) FetchLatestVersionFromHost() {
	m.mu.Lock()
	if m.inflight != nil {
		// Already fetching
		m.mu.Unlock()
		return
	}
	ch := make(chan struct{})
	m.inflight = ch
	m.mu.Unlock()

	go m.fetchLatestVersion()
}

// ReportError synchronously reports err.Error() to the server, along with metadata (nil allowed). An error is returned for
// connection issues.
//
// The request: POST Host+ReportErrorPath with JSON body: {"error": err.Error(), "metadata": metadata, "host": HostProperties(),
// "props": m.stableProps}.
func (m *Monitor) ReportError(err error, metadata map[string]string) error {
	if !m.isErrorReportingEnabled() {
		return nil
	}
	if err == nil {
		return nil
	}
	if m.ReportErrorPath == "" {
		return nil
	}
	u, ok := m.buildURL(m.ReportErrorPath, "")
	if !ok {
		return errors.New("invalid host or URL for ReportError")
	}

	payload := map[string]any{
		"error":    err.Error(),
		"metadata": safeCopyMap(metadata),
		"host":     HostProperties(),
		"props":    m.copyStableProps(),
	}
	body, _ := json.Marshal(payload)
	req, reqErr := http.NewRequest(http.MethodPost, u, bytes.NewReader(body))
	if reqErr != nil {
		return reqErr
	}
	req.Header.Set("Content-Type", "application/json")
	if bt := strings.TrimSpace(m.BuildToken); bt != "" {
		req.Header.Set("Build-Token", bt)
	}
	resp, httpErr := m.client().Do(req)
	if httpErr != nil {
		return httpErr
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("report error: http %d", resp.StatusCode)
	}
	return nil
}

// ReportPanic synchronously reports panicVal and stack to the host, along with metadata (nil allowed). An error is returned
// for connection issues or non-2xx HTTP status codes.
//
// The request: POST Host+ReportPanicPath, with JSON body: {"panic": fmt.Sprintf("%v", panicVal), "stack": stack, "metadata":
// metadata, "host": hostProps, "props": m.stableProps}.
func (m *Monitor) ReportPanic(panicVal any, stack []byte, metadata map[string]string) error {
	if !m.isPanicReportingEnabled() {
		return nil
	}
	if m.ReportPanicPath == "" {
		return nil
	}
	u, ok := m.buildURL(m.ReportPanicPath, "")
	if !ok {
		return errors.New("invalid host or URL for ReportPanic")
	}

	payload := map[string]any{
		"panic":    fmt.Sprintf("%v", panicVal),
		"stack":    string(stack),
		"metadata": safeCopyMap(metadata),
		"host":     HostProperties(),
		"props":    m.copyStableProps(),
	}
	body, _ := json.Marshal(payload)
	req, reqErr := http.NewRequest(http.MethodPost, u, bytes.NewReader(body))
	if reqErr != nil {
		return reqErr
	}
	req.Header.Set("Content-Type", "application/json")
	if bt := strings.TrimSpace(m.BuildToken); bt != "" {
		req.Header.Set("Build-Token", bt)
	}
	resp, httpErr := m.client().Do(req)
	if httpErr != nil {
		return httpErr
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("report panic: http %d", resp.StatusCode)
	}
	return nil
}

// WithPanicReporting executes f and recovers any panic, reporting it via ReportPanic and returning an error instead of
// re-panicking. If reporting fails, the returned error includes the reporting error as context. If f does not panic, the
// returned error is nil.
func (m *Monitor) WithPanicReporting(f func()) (err error) {
	defer func() {
		if r := recover(); r != nil {
			// Attempt to report; tolerate nil receiver
			var repErr error
			if m != nil {
				repErr = m.ReportPanic(r, debug.Stack(), nil)
			}
			if repErr != nil {
				err = fmt.Errorf("panic recovered: %v (report error: %v)", r, repErr)
			} else {
				err = fmt.Errorf("panic recovered: %v", r)
			}
		}
	}()
	f()
	return nil
}

// ReportEventAsync asynchronously reports event to the server, along with the metadata. It returns immediately.
//
// The request: GET Host+ReportEventPath + toquery(metadata).
//
// Note that NO HostProperties are sent by default. The query parameters are event and metadata, and if includeStableProps,
// then also the stable props.
func (m *Monitor) ReportEventAsync(event string, metadata map[string]string, includeStableProps bool) {
	if !m.isEventReportingEnabled() {
		return
	}
	if m.ReportEventPath == "" {
		return
	}
	u, ok := m.buildURL(m.ReportEventPath, "")
	if !ok {
		return
	}

	// Build query
	q := url.Values{}
	// Include timestamp in milliseconds since Unix epoch
	q.Set("ts", strconv.FormatInt(time.Now().UnixMilli(), 10))
	if event != "" {
		q.Set("e", event)
	}
	for k, v := range metadata {
		if k == "" {
			continue
		}
		q.Set(k, v)
	}
	if includeStableProps {
		for k, v := range m.copyStableProps() {
			q.Set(k, v)
		}
	}

	full := u
	if strings.Contains(full, "?") {
		full += "&" + q.Encode()
	} else {
		full += "?" + q.Encode()
	}

	go func(urlStr string) {
		req, _ := http.NewRequest(http.MethodGet, urlStr, nil)
		if bt := strings.TrimSpace(m.BuildToken); bt != "" {
			req.Header.Set("Build-Token", bt)
		}
		resp, err := m.client().Do(req)
		if err != nil {
			return
		}
		defer resp.Body.Close()
		io.Copy(io.Discard, resp.Body)
	}(full)
}

// isPanicReportingEnabled reports whether panic reporting is enabled. Defaults to true when not configured.
// isErrorReportingEnabled reports whether error reporting is enabled. Defaults to true when not configured.
func (m *Monitor) isErrorReportingEnabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return !m.errorReportingDisabled
}

// isEventReportingEnabled reports whether event reporting is enabled. Defaults to true when not configured.
func (m *Monitor) isEventReportingEnabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return !m.eventReportingDisabled
}

// isPanicReportingEnabled reports whether panic reporting is enabled. Defaults to true when not configured.
func (m *Monitor) isPanicReportingEnabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return !m.panicReportingDisabled
}

// ErrNotCached indicates there is no cached latest version.
var ErrNotCached = errors.New("latest version not cached")

// client returns the HTTP client used by m, creating one with a 4-second timeout if none is set. It is safe for concurrent
// use. To customize the client, assign m.httpClient before invoking networked methods. The receiver must be non-nil.
func (m *Monitor) client() *http.Client {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.httpClient == nil {
		m.httpClient = &http.Client{Timeout: 4 * time.Second}
	}
	return m.httpClient
}

// copyStableProps returns a shallow copy of the stable properties, or nil if none are set. It is safe for concurrent use;
// the returned map may be modified by the caller.
func (m *Monitor) copyStableProps() map[string]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.stableProps) == 0 {
		return nil
	}
	cp := make(map[string]string, len(m.stableProps))
	maps.Copy(cp, m.stableProps)
	return cp
}

// safeCopyMap returns a shallow copy of in, or nil if in is nil or empty. It is safe to pass a nil map; the function returns
// nil in that case.
func safeCopyMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	cp := make(map[string]string, len(in))
	maps.Copy(cp, in)
	return cp
}

// buildURL returns an absolute URL string. If pathOrURL is absolute (http/https), it is used as-is. If it's a path, it is
// combined with m.Host. defaultPath is used when pathOrURL is empty.
func (m *Monitor) buildURL(pathOrURL, defaultPath string) (string, bool) {
	s := strings.TrimSpace(pathOrURL)
	if s == "" {
		s = strings.TrimSpace(defaultPath)
	}
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return s, true
	}
	host := strings.TrimSpace(m.Host)
	if host == "" {
		return "", false
	}
	// Join with single slash
	left := strings.TrimRight(host, "/")
	right := strings.TrimLeft(s, "/")
	return left + "/" + right, true
}

// fetchLatestVersion builds the request URL and, if valid, performs the network request and updates cache; it signals inflight
// when done. If URL construction fails, it records an error and returns without making a request.
func (m *Monitor) fetchLatestVersion() {
	// Determine URL outside lock where possible
	u, ok := m.buildURL(m.LatestVersionURL, "/version")
	if !ok {
		m.mu.Lock()
		if m.inflight != nil {
			close(m.inflight)
			m.inflight = nil
		}
		m.latestVersion = ""
		m.latestVersionErr = errors.New("invalid host or LatestVersionURL")
		m.mu.Unlock()
		return
	}

	// Perform request
	var (
		version string
		reqErr  error
	)
	req, _ := http.NewRequest(http.MethodGet, u, nil)
	if bt := strings.TrimSpace(m.BuildToken); bt != "" {
		req.Header.Set("Build-Token", bt)
	}
	resp, err := m.client().Do(req)
	if err != nil {
		reqErr = err
	} else {
		defer resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			// Expect JSON: {"version":"x"}
			var vr struct {
				Version string `json:"version"`
			}
			dec := json.NewDecoder(resp.Body)
			if err := dec.Decode(&vr); err != nil {
				reqErr = err
			} else {
				version = strings.TrimSpace(vr.Version)
				if version == "" {
					reqErr = errors.New("empty version from server")
				}
			}
		} else {
			reqErr = fmt.Errorf("latest version http %d", resp.StatusCode)
		}
	}

	// Update cache and signal waiters
	m.mu.Lock()
	m.latestVersion = version
	m.latestVersionErr = reqErr
	if m.inflight != nil {
		close(m.inflight)
		m.inflight = nil
	}
	m.mu.Unlock()
}

// HostProperties returns a map of k/vs relating to the host environment.
//
// All values should be considered anonymous - no personally identifiable info is returned. TZ/Locale are allowed, as even
// though they narrow down, they can't identify users.
//
// Never return things like their ENV, username, IP/MAC, filenames, or path info, etc.
func HostProperties() map[string]string {
	ret := map[string]string{
		"time": time.Now().UTC().Format(time.RFC3339),

		"goos":   runtime.GOOS,
		"goarch": runtime.GOARCH,

		"numcpu":        strconv.Itoa(runtime.NumCPU()),
		"gomaxprocs":    strconv.Itoa(runtime.GOMAXPROCS(0)),
		"numgoroutines": strconv.Itoa(runtime.NumGoroutine()),
		"mem_alloc":     "",
		"mem_sys":       "",

		"tz": "",
		// "locale": "", NOTE: we could do this, but it's more system calls to do in cross-platform way; I'm just punting for now.

		"os_flavor": "", // os and version.
		"container": "", // "true" if we think it's likely we're running in a container, toherwise false
	}

	// Memory stats
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	ret["mem_alloc"] = strconv.FormatUint(ms.Alloc, 10)
	ret["mem_sys"] = strconv.FormatUint(ms.Sys, 10)

	// Timezone (abbrev with numeric offset fallback)
	if name, offset := time.Now().Zone(); name != "" && name != "Local" {
		ret["tz"] = name
	} else {
		// Fallback: UTC±HH:MM
		o := offset
		sign := "+"
		if o < 0 {
			sign = "-"
			o = -o
		}
		hours := o / 3600
		mins := (o % 3600) / 60
		ret["tz"] = "UTC" + sign + twoDigit(hours) + ":" + twoDigit(mins)
	}

	// OS flavor
	switch runtime.GOOS {
	case "linux":
		if v := readOSReleasePrettyName(); v != "" {
			ret["os_flavor"] = v
		}
	case "darwin":
		if v := readDarwinOSFlavor(); v != "" {
			ret["os_flavor"] = v
		}
	case "windows":
		if v := readWindowsOSFlavor(); v != "" {
			ret["os_flavor"] = v
		}
	}

	// Container heuristic
	if isLikelyContainer() {
		ret["container"] = "true"
	} else {
		ret["container"] = "false"
	}

	return ret
}

// twoDigit converts small ints to zero-padded 2-digit strings without fmt.
func twoDigit(i int) string {
	if i < 10 {
		return "0" + strconv.Itoa(i)
	}
	return strconv.Itoa(i)
}

// readOSReleasePrettyName returns PRETTY_NAME or NAME+VERSION from /etc/os-release when available.
func readOSReleasePrettyName() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil || len(data) == 0 {
		return ""
	}
	var name, version, pretty string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if key, val, ok := splitKeyVal(line); ok {
			switch key {
			case "PRETTY_NAME":
				pretty = val
			case "NAME":
				name = val
			case "VERSION":
				version = val
			}
		}
	}
	if pretty != "" {
		return pretty
	}
	if name != "" && version != "" {
		return name + " " + version
	}
	return name
}

// splitKeyVal parses KEY=VALUE lines where VALUE may be quoted.
func splitKeyVal(line string) (string, string, bool) {
	eq := strings.IndexByte(line, '=')
	if eq <= 0 || eq+1 >= len(line) {
		return "", "", false
	}
	key := strings.TrimSpace(line[:eq])
	val := strings.TrimSpace(line[eq+1:])
	val = strings.Trim(val, "\"'")
	return key, val, true
}

// isLikelyContainer detects common container environments without PII.
func isLikelyContainer() bool {
	// Well-known files
	if fileExists("/.dockerenv") || fileExists("/run/.containerenv") {
		return true
	}
	// cgroup hints (Linux)
	for _, p := range []string{"/proc/1/cgroup", "/proc/self/cgroup"} {
		if data, err := os.ReadFile(p); err == nil {
			s := string(data)
			if strings.Contains(s, "docker") || strings.Contains(s, "kubepods") || strings.Contains(s, "containerd") || strings.Contains(s, "lxc") {
				return true
			}
		}
	}
	return false
}

// fileExists reports whether path names an existing non-directory file. It returns false if the file does not exist, is
// a directory, or if an error occurs.
func fileExists(path string) bool {
	if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
		return true
	}
	return false
}

// readDarwinOSFlavor returns a short macOS flavor string derived from sw_vers.
//
// It returns "osx <version>" when ProductVersion is found, "osx" if the version is missing, or an empty string on failure.
// It does not read ProductName.
func readDarwinOSFlavor() string {
	out, err := exec.Command("sw_vers").Output()
	if err != nil || len(out) == 0 {
		return ""
	}
	var version string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "ProductVersion:"); ok {
			version = strings.TrimSpace(after)
			break
		}
	}
	if version != "" {
		return "osx " + version
	}
	return "osx"
}

// readWindowsOSFlavor returns a normalized Windows OS flavor string from the registry via reg.exe. It derives a lowercase
// series such as "windows 11" or "windows 10" from ProductName and omits the edition. When available, it appends DisplayVersion
// or ReleaseId; otherwise, it returns only the series.
func readWindowsOSFlavor() string {
	const key = "HKLM\\SOFTWARE\\Microsoft\\Windows NT\\CurrentVersion"
	// Single query; if it fails, don't try again.
	out, err := exec.Command("cmd", "/c", "reg", "query", key).Output()
	if err != nil || len(out) == 0 {
		return ""
	}

	var product, display, releaseID string
	for _, line := range strings.Split(string(out), "\n") {
		l := strings.TrimSpace(line)
		if strings.HasPrefix(l, "ProductName") {
			product = extractRegValue(l)
			continue
		}
		if strings.HasPrefix(l, "DisplayVersion") {
			display = extractRegValue(l)
			continue
		}
		if strings.HasPrefix(l, "ReleaseId") {
			releaseID = extractRegValue(l)
			continue
		}
	}

	// Decide major series from ProductName; strip edition.
	series := "windows"
	if strings.Contains(strings.ToLower(product), "windows 11") {
		series = "windows 11"
	} else if strings.Contains(strings.ToLower(product), "windows 10") {
		series = "windows 10"
	}

	// Prefer DisplayVersion, then ReleaseId. If neither, just series.
	ver := display
	if ver == "" {
		ver = releaseID
	}

	if ver != "" {
		return series + " " + ver
	}
	return series
}

// extractRegValue parses a single 'reg query' output line and returns the value text. Example line:
//
//	ProductName REG_SZ Windows 11 Pro
func extractRegValue(line string) string {
	idx := strings.Index(line, "REG_")
	if idx == -1 {
		return ""
	}
	s := strings.TrimSpace(line[idx:])
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return ""
	}
	drop := fields[0]
	pos := strings.Index(line, drop)
	if pos == -1 {
		return ""
	}
	return strings.TrimSpace(line[pos+len(drop):])
}
