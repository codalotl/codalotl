package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codalotl/codalotl/internal/docubot"
	"github.com/codalotl/codalotl/internal/gocas"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/stretchr/testify/require"
)

func TestRun_DocsStatus_PrintsPerPackageStatus(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()
	createGitRepoMarker(t, tmp)
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, ".codalotl"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, ".codalotl", "config.json"), []byte(`{"reflowwidth":40}`+"\n"), 0644))

	writePackageFile(t, tmp, "p1", `package p1

// Foo does a thing.
func Foo() {}
`)
	p2Source := `package p2

// Bar does a thing. This is a deliberately long documentation sentence that should be wrapped by the status command dry-run check when width is small.
func Bar() {}
`
	writePackageFile(t, tmp, "p2", p2Source)
	writePackageFile(t, tmp, "p3", `package p3

// Baz does a thing.
func Baz() {}
`)

	t.Setenv(gocas.EnvCASDB, filepath.Join(tmp, "casdb"))
	storeCASTestRecord(t, tmp, "docs-fix", "p1", docsFixCASValue{
		Schema:   string(docsFixCASNamespaceSpec.Namespace()),
		Mode:     docsFixModeWholePackage,
		FixCount: 0,
	})
	storeCASTestRecord(t, tmp, "docs-fix", "p2", docsFixCASValue{
		Schema:      string(docsFixCASNamespaceSpec.Namespace()),
		Mode:        docsFixModeIdentifiers,
		Identifiers: []string{"Bar"},
		FixCount:    0,
	})
	p2Path := filepath.Join(tmp, "p2", "p2.go")
	p2ModTime := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	require.NoError(t, os.Chtimes(p2Path, p2ModTime, p2ModTime))
	p2StatBefore, err := os.Stat(p2Path)
	require.NoError(t, err)

	origCountMissingDocs := runDocubotCountMissingDocs
	t.Cleanup(func() { runDocubotCountMissingDocs = origCountMissingDocs })
	countCalls := map[string]bool{}
	runDocubotCountMissingDocs = func(pkg *gocode.Package, opts docubot.AddDocsOptions) (int, error) {
		require.True(t, opts.OnlyDocumentImportantIdentifiers)
		require.False(t, opts.OnlyDocumentExportedIdentifiers)
		require.False(t, opts.DocumentTestFiles)
		countCalls[pkg.ImportPath] = true

		switch filepath.Base(pkg.AbsolutePath()) {
		case "p1":
			return 0, nil
		case "p2":
			return 1, nil
		case "p3":
			return 0, errors.New("count failed")
		default:
			t.Fatalf("unexpected package: %s", pkg.ImportPath)
			return 0, nil
		}
	}

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "docs", "status"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())
	require.Equal(t, map[string]bool{
		"example.com/tmpmod/p1": true,
		"example.com/tmpmod/p2": true,
		"example.com/tmpmod/p3": true,
	}, countCalls)

	rows, order := parseDocsStatusRows(out.String())
	require.Equal(t, []string{"./p1", "./p2", "./p3"}, order)
	require.Equal(t, docsStatusTestRow{docsAdd: "current", docsFix: "current", reflow: "current"}, rows["./p1"])
	require.Equal(t, docsStatusTestRow{docsAdd: "needed", docsFix: "needed", reflow: "needed"}, rows["./p2"])
	require.Equal(t, docsStatusTestRow{docsAdd: "error", docsFix: "needed", reflow: "current"}, rows["./p3"])

	gotP2, err := os.ReadFile(p2Path)
	require.NoError(t, err)
	require.Equal(t, p2Source, string(gotP2))
	p2StatAfter, err := os.Stat(p2Path)
	require.NoError(t, err)
	require.Equal(t, p2StatBefore.Mode(), p2StatAfter.Mode())
	require.Equal(t, p2StatBefore.ModTime(), p2StatAfter.ModTime())
}

type docsStatusTestRow struct {
	docsAdd string
	docsFix string
	reflow  string
}

func parseDocsStatusRows(s string) (map[string]docsStatusTestRow, []string) {
	rows := map[string]docsStatusTestRow{}
	var order []string
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 4 {
			continue
		}
		if !strings.HasPrefix(fields[0], ".") {
			continue
		}
		pkg := fields[0]
		rows[pkg] = docsStatusTestRow{docsAdd: fields[1], docsFix: fields[2], reflow: fields[3]}
		order = append(order, pkg)
	}
	return rows, order
}
