package integration

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIntegrationCases(t *testing.T) {
	caseNames, err := ListCaseNames(casesRoot)
	require.NoError(t, err)

	for _, caseName := range caseNames {
		t.Run(caseName, func(t *testing.T) {
			require.NoError(t, RunCaseDir(filepath.Join(casesRoot, caseName)))
		})
	}
}
