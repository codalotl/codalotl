package renamebot

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/codalotl/codalotl/internal/gocode"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenames(t *testing.T) {
	t.Skip()
	mod, err := gocode.NewModule(gocode.MustCwd())
	require.NoError(t, err)

	pkg, err := mod.LoadPackageByRelativeDir("internal/gocode")
	require.NoError(t, err)

	err = RenameForConsistency(pkg, BaseOptions{})
	assert.NoError(t, err)
}

func TestApplication(t *testing.T) {
	t.Skip()

	f, err := os.Open("testdata/responses1.txt")
	require.NoError(t, err)
	defer f.Close()

	dec := json.NewDecoder(f)
	var all []ProposedRename
	for {
		var arr []ProposedRename
		if err := dec.Decode(&arr); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			require.NoError(t, err)
		} else {
			if len(arr) > 0 {
				all = append(all, arr...)
			}
		}
	}

	mod, err := gocode.NewModule(gocode.MustCwd())
	require.NoError(t, err)

	pkg, err := mod.LoadPackageByRelativeDir("codeai/gocode")
	require.NoError(t, err)

	// Backfill missing File on each ProposedRename by looking up the snippet for FuncID.
	for i := range all {
		if all[i].File != "" {
			continue
		}
		s := pkg.GetSnippet(all[i].FuncID)
		if s == nil && pkg.TestPackage != nil {
			s = pkg.TestPackage.GetSnippet(all[i].FuncID)
		}
		require.NotNilf(t, s, "could not resolve snippet for FuncID=%q", all[i].FuncID)
		fileName := s.Position().Filename
		require.NotEmptyf(t, fileName, "no filename for FuncID=%q", all[i].FuncID)
		all[i].File = fileName
	}

	err = applyRenames(pkg, all)
	assert.NoError(t, err)
}
