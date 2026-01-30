package renamebot

import (
	"strings"
	"testing"

	"github.com/codalotl/codalotl/internal/gocode"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSummaryReflexive(t *testing.T) {
	// t.Skip()
	mod, err := gocode.NewModule(gocode.MustCwd())
	require.NoError(t, err)

	pkg, err := mod.LoadPackageByRelativeDir("internal/renamebot")
	require.NoError(t, err)

	summary, err := newPackageSummary(pkg, false)
	require.NoError(t, err)

	// Helper to parse root type headers from the printed summary.
	parseRootTypes := func(s string) map[string]struct{} {
		out := map[string]struct{}{}
		for _, line := range strings.Split(s, "\n") {
			if line == "" {
				continue
			}
			// Root type headers are unindented and end with a colon, e.g. "go/types.Type:"
			if strings.HasSuffix(line, ":") && (len(line) > 0 && (line[0] != ' ' && line[0] != '\t')) {
				// strip trailing ':'
				out[strings.TrimSuffix(line, ":")] = struct{}{}
			}
		}
		return out
	}

	beforeStr := summary.String()
	before := parseRootTypes(beforeStr)
	require.Greater(t, len(before), 0, "expected at least one root type before rejectUnified")

	// Apply rejection and re-parse
	summary.rejectUnified()
	afterStr := summary.String()
	after := parseRootTypes(afterStr)

	// After should be a subset of before (rejectUnified never adds types)
	for rt := range after {
		_, ok := before[rt]
		assert.Truef(t, ok, "unexpected root type after rejectUnified: %s", rt)
	}

	// Ensure at least one type was removed (reflexive to current code without naming a specific type).
	removed := 0
	for rt := range before {
		if _, ok := after[rt]; !ok {
			removed++
		}
	}
	assert.Greater(t, removed, 0, "expected rejectUnified to remove at least one root type")

	tiSummary := summary.relevantForFile("typed_identifiers.go")

	// make sure summary isn't mutated and filtering changes the view size
	assert.True(t, tiSummary != summary)
	assert.True(t, len(tiSummary.summaryPerType) <= len(summary.summaryPerType))

	// Expected set is the intersection of the file's root types and the remaining summaries after rejection.
	wantSet := map[string]struct{}{}
	for rt := range summary.fileToRootTypeSet["typed_identifiers.go"] {
		if _, ok := summary.summaryPerType[rt]; ok {
			wantSet[rt] = struct{}{}
		}
	}
	gotSet := map[string]struct{}{}
	for rt := range tiSummary.summaryPerType {
		gotSet[rt] = struct{}{}
	}
	assert.Equal(t, wantSet, gotSet)
}

func TestRelevantForFile(t *testing.T) {
	mod, err := gocode.NewModule(gocode.MustCwd())
	require.NoError(t, err)

	pkg, err := mod.LoadPackageByRelativeDir("internal/gocode")
	require.NoError(t, err)

	// Build full package summary (non-tests)
	full, err := newPackageSummary(pkg, false)
	require.NoError(t, err)
	require.NotNil(t, full)

	fileName := "group.go"
	if _, ok := pkg.Files[fileName]; !ok {
		t.Fatalf("expected file %q to exist in package", fileName)
	}

	// Expected root types for the file from the full summary's fileToRootTypeSet
	wantSet := full.fileToRootTypeSet[fileName]
	// Guard: if no types, just ensure result is empty
	filtered := full.relevantForFile(fileName)
	require.NotNil(t, filtered)

	// Compare kept root types
	gotSet := map[string]struct{}{}
	for rt := range filtered.summaryPerType {
		gotSet[rt] = struct{}{}
	}
	require.Equal(t, wantSet, gotSet, "filtered summary should keep exactly the file's root types")

	// Verify counts for each kept type are preserved (aggregated across entire package)
	for rt := range wantSet {
		orig := full.summaryPerType[rt]
		filt := filtered.summaryPerType[rt]
		if orig == nil || filt == nil {
			t.Fatalf("missing type summary for %s", rt)
		}
		sum := func(m map[typeSummaryKey]int) int {
			total := 0
			for _, c := range m {
				total += c
			}
			return total
		}
		require.Equal(t, sum(orig.all), sum(filt.all), "total entries for %s should be preserved", rt)
		require.Equal(t, sum(orig.funcVars), sum(filt.funcVars), "func var entries for %s should be preserved", rt)
		require.Equal(t, sum(orig.params), sum(filt.params), "param entries for %s should be preserved", rt)
		require.Equal(t, sum(orig.receivers), sum(filt.receivers), "receiver entries for %s should be preserved", rt)
	}
}
