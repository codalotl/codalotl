package tui

import (
	"fmt"
	"strings"

	"github.com/codalotl/codalotl/internal/q/semver"
)

type latestVersionMsg struct {
	latest string
	err    error
}

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
