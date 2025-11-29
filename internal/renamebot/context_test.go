package renamebot

import (
	"fmt"
	"github.com/codalotl/codalotl/internal/gocode"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContextForFileReflexive(t *testing.T) {
	t.Skip()
	mod, err := gocode.NewModule(gocode.MustCwd())
	require.NoError(t, err)

	pkg, err := mod.LoadPackageByRelativeDir("codeai/gocode")
	require.NoError(t, err)

	summary, err := newPackageSummary(pkg, false)
	require.NoError(t, err)
	summary.rejectUnified()

	context := contextForFile(pkg, pkg.Files["package.go"], summary)

	fmt.Println(context)

	renames, err := askLLMForRenames(context, BaseOptions{})
	assert.NoError(t, err)

	for _, r := range renames {
		fmt.Println(r)
	}

}
