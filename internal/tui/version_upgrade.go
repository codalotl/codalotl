package tui

import (
	"fmt"
	"strings"

	"github.com/codalotl/codalotl/internal/q/semver"
)

// latestVersionMsg delivers the result of a latest-version check.
type latestVersionMsg struct {
	latest string // latest is the newest version reported by the remote monitor.
	err    error  // err is the version check error, if the check failed.
}

// versionUpgradeNoticeSection returns the Info Panel update notice, if one should be shown.
func (m *model) versionUpgradeNoticeSection() string {
	if m == nil || m.monitor == nil {
		return ""
	}
	return formatVersionUpgradeNotice(infoPanelContentWidth(m.infoPanelWidth), m.monitor.CurrentVersion, m.latestVersion)
}

func infoPanelContentWidth(totalWidth int) int {
	// infoPanelBlock uses a left and right border plus padding of 1 on each side.
	// i.e., the "content" width is total - (2 border) - (2 padding).
	contentWidth := totalWidth - 4
	if contentWidth <= 0 {
		return 1
	}
	return contentWidth
}

// formatVersionUpgradeNotice returns the info-panel update notice for a newer latestVersion. It returns an empty string when either version cannot be compared,
// currentVersion is empty or "dev", latestVersion is empty, or latestVersion is not greater than currentVersion. A positive contentWidth wraps the notice to that
// width; a non-positive width leaves the notice unwrapped.
func formatVersionUpgradeNotice(contentWidth int, currentVersion string, latestVersion string) string {
	if strings.TrimSpace(latestVersion) == "" {
		return ""
	}

	currentTrimmed := strings.TrimSpace(currentVersion)
	if currentTrimmed == "" || currentTrimmed == "dev" {
		return ""
	}

	current, err := semver.Parse(currentVersion)
	if err != nil {
		return ""
	}
	latest, err := semver.Parse(latestVersion)
	if err != nil {
		return ""
	}
	if !latest.GreaterThan(current) {
		return ""
	}

	lines := []string{
		fmt.Sprintf("An update is available: %s (current %s)", latest.String(), current.String()),
		"Run go install github.com/codalotl/codalotl@latest",
	}

	if contentWidth <= 0 {
		return strings.Join(lines, "\n")
	}

	var out []string
	for _, l := range lines {
		out = append(out, wrapWords(contentWidth, strings.Fields(l))...)
	}
	return strings.Join(out, "\n")
}
