package tui

import (
	"path/filepath"
	"strings"

	"github.com/codalotl/codalotl/internal/gocas"
	"github.com/codalotl/codalotl/internal/gocas/casconformance"
)

type specConformanceState struct {
	runID    int
	checked  bool
	found    bool
	conforms bool
}

func (m *model) shouldShowSpecConformance() bool {
	if m == nil || m.casDB == nil {
		return false
	}

	pkgPath := ""
	switch {
	case m.session != nil && strings.TrimSpace(m.session.packagePath) != "":
		pkgPath = m.session.packagePath
	default:
		pkgPath = m.sessionConfig.packagePath
	}
	return strings.TrimSpace(pkgPath) != ""
}

func (m *model) specConformanceIndicator() string {
	if m == nil || m.specConformance == nil {
		return "-"
	}
	if m.specConformance.checked && m.specConformance.found && m.specConformance.conforms {
		return "✓"
	}
	return "-"
}

func (m *model) startSpecConformanceCheck() {
	if m == nil || m.tui == nil {
		return
	}
	if !m.shouldShowSpecConformance() {
		m.specConformance = nil
		return
	}

	// Spec conformance is package-derived metadata; without a real session / package
	// abs path we can't compute it. In normal app flow this is always present.
	if m.session == nil || !m.session.config.packageMode() || strings.TrimSpace(m.session.packageAbsPath) == "" {
		return
	}

	absRoot := strings.TrimSpace(m.casDB.AbsRoot)
	if absRoot == "" || !filepath.IsAbs(absRoot) {
		debugLogf("spec conformance check disabled: invalid CAS root %q", absRoot)
		return
	}

	m.nextSpecConformanceID++
	runID := m.nextSpecConformanceID
	m.specConformance = &specConformanceState{runID: runID}

	sandboxDir := m.session.sandboxDir
	pkgAbsPath := m.session.packageAbsPath
	prog := m.tui
	go func() {
		found := false
		conforms := false
		errMsg := ""

		pkg, err := loadGoPackage(pkgAbsPath)
		if err != nil {
			errMsg = err.Error()
		} else {
			db := &gocas.DB{BaseDir: sandboxDir, DB: *m.casDB}
			found, conforms, err = casconformance.Retrieve(db, pkg)
			if err != nil {
				errMsg = err.Error()
				found = false
				conforms = false
			}
		}

		prog.Send(specConformanceResultMsg{
			runID:    runID,
			found:    found,
			conforms: conforms,
			errMsg:   errMsg,
		})
	}()
}

func (m *model) handleSpecConformanceResult(msg specConformanceResultMsg) {
	if m == nil || m.specConformance == nil || m.specConformance.runID != msg.runID {
		return
	}
	m.specConformance.checked = true
	m.specConformance.found = msg.found
	m.specConformance.conforms = msg.conforms

	if msg.errMsg != "" {
		debugLogf("spec conformance check failed: %s", msg.errMsg)
	}
}
