package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func helpListsCommand(helpOutput string, name string) bool {
	for _, line := range strings.Split(helpOutput, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == name {
			return true
		}
	}
	return false
}

func TestRun_Help_DoesNotListPanicCommand(t *testing.T) {
	isolateUserConfig(t)

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "-h"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())

	require.False(t, helpListsCommand(out.String(), "panic"))
}

func TestRun_PanicCommand_Panics(t *testing.T) {
	isolateUserConfig(t)

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "panic"}, &RunOptions{Out: &out, Err: &errOut})
	require.Error(t, err)
	require.Equal(t, 1, code)

	require.Contains(t, err.Error(), "intentional panic")
	require.Contains(t, errOut.String(), "intentional panic")
}
