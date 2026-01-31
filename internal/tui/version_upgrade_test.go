package tui

import (
	"testing"

	"github.com/codalotl/codalotl/internal/q/remotemonitor"

	"github.com/stretchr/testify/assert"
)

func TestFormatVersionUpgradeNotice(t *testing.T) {
	got := formatVersionUpgradeNotice(80, "1.2.3", "1.2.4")
	assert.Equal(t, "An update is available: 1.2.4 (current 1.2.3)\nRun go install github.com/codalotl/codalotl@latest", got)
}

func TestFormatVersionUpgradeNotice_NormalizesVPrefix(t *testing.T) {
	got := formatVersionUpgradeNotice(80, "v1.2.3", "v1.2.4")
	assert.Equal(t, "An update is available: 1.2.4 (current 1.2.3)\nRun go install github.com/codalotl/codalotl@latest", got)
}

func TestFormatVersionUpgradeNotice_NoUpgrade(t *testing.T) {
	assert.Empty(t, formatVersionUpgradeNotice(80, "1.2.3", "1.2.3"))
	assert.Empty(t, formatVersionUpgradeNotice(80, "1.2.3", "1.2.2"))
}

func TestFormatVersionUpgradeNotice_InvalidVersionOrDev(t *testing.T) {
	assert.Empty(t, formatVersionUpgradeNotice(80, "dev", "1.2.4"))
	assert.Empty(t, formatVersionUpgradeNotice(80, "1.2.3", "not-a-version"))
	assert.Empty(t, formatVersionUpgradeNotice(80, "not-a-version", "1.2.4"))
}

func TestInfoPanelContentWidth(t *testing.T) {
	assert.Equal(t, 36, infoPanelContentWidth(40))
	assert.Equal(t, 1, infoPanelContentWidth(4))
	assert.Equal(t, 1, infoPanelContentWidth(0))
}

func TestModelVersionUpgradeNoticeSection(t *testing.T) {
	m := &model{
		infoPanelWidth: 60,
		monitor:        &remotemonitor.Monitor{CurrentVersion: "1.2.3"},
		latestVersion:  "1.2.4",
	}

	got := m.versionUpgradeNoticeSection()
	assert.Equal(t, "An update is available: 1.2.4 (current 1.2.3)\nRun go install github.com/codalotl/codalotl@latest", got)
}
