package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/codalotl/codalotl/internal/docubot"
	"github.com/codalotl/codalotl/internal/gocas"
	"github.com/codalotl/codalotl/internal/gocas/casclarify"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/stretchr/testify/require"
)

func TestRun_DocsImproveFromClarify_ProcessesRecordsAndDeletesNoops(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, ".codalotl"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, ".codalotl", "config.json"), []byte(`{
  "preferredprovider": "anthropic",
  "reflowwidth": 77
}`), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "p"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "p", "p.go"), []byte("package p\n\nfunc Foo() {}\n\nfunc Bar() {}\n"), 0644))

	t.Setenv("CODALOTL_CAS_DB", filepath.Join(tmp, "casdb"))

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	recordPath1 := filepath.Join(tmp, "record-1.json")
	recordPath2 := filepath.Join(tmp, "record-2.json")
	require.NoError(t, os.WriteFile(recordPath1, []byte("{}"), 0644))
	require.NoError(t, os.WriteFile(recordPath2, []byte("{}"), 0644))

	origFind := findInPlayClarifyRecords
	origImprove := runDocubotImproveFromClarifications
	t.Cleanup(func() {
		findInPlayClarifyRecords = origFind
		runDocubotImproveFromClarifications = origImprove
	})

	findInPlayClarifyRecords = func(db *gocas.DB, mod *gocode.Module) ([]casclarify.InPlayRecord, error) {
		wantModuleDir, err := filepath.EvalSymlinks(tmp)
		require.NoError(t, err)
		gotModuleDir, err := filepath.EvalSymlinks(mod.AbsolutePath)
		require.NoError(t, err)
		gotBaseDir, err := filepath.EvalSymlinks(db.BaseDir)
		require.NoError(t, err)
		require.Equal(t, wantModuleDir, gotModuleDir)
		require.Equal(t, wantModuleDir, gotBaseDir)
		return []casclarify.InPlayRecord{
			{
				Path:          recordPath1,
				TargetPackage: "example.com/tmpmod/p",
				Metadata: casclarify.Metadata{Entries: []casclarify.Entry{
					{
						Identifier: "Foo",
						Question:   "What does Foo do?",
						Answer:     "Foo does the foo thing.",
					},
					{
						TargetPackage: "example.com/tmpmod/p",
						Identifier:    "Bar",
						Question:      "What does Bar do?",
						Answer:        "Bar does the bar thing.",
					},
				}},
			},
			{
				Path:          recordPath2,
				TargetPackage: "example.com/tmpmod/p",
				Metadata: casclarify.Metadata{Entries: []casclarify.Entry{
					{
						Identifier: "Foo",
						Question:   "Any extra details?",
						Answer:     "No extra documentation is needed.",
					},
				}},
			},
		}, nil
	}

	type improveCall struct {
		pkg            *gocode.Package
		clarifications []docubot.Clarification
		opts           docubot.ImproveFromClarificationsOptions
	}
	var calls []improveCall
	runDocubotImproveFromClarifications = func(pkg *gocode.Package, clarifications []docubot.Clarification, opts docubot.ImproveFromClarificationsOptions) ([]docubot.IncorporatedFeedback, error) {
		calls = append(calls, improveCall{
			pkg:            pkg,
			clarifications: append([]docubot.Clarification(nil), clarifications...),
			opts:           opts,
		})
		if len(calls) == 1 {
			return []docubot.IncorporatedFeedback{{}}, nil
		}
		return nil, nil
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "docs", "improve-from-clarify"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())
	require.Equal(t, "Processed 2 clarify record(s); deleted 2; applied 1 documentation change(s).\n", out.String())

	_, err = os.Stat(recordPath1)
	require.True(t, os.IsNotExist(err))
	_, err = os.Stat(recordPath2)
	require.True(t, os.IsNotExist(err))

	require.Len(t, calls, 2)
	require.Equal(t, "example.com/tmpmod/p", calls[0].pkg.ImportPath)
	require.Equal(t, []docubot.Clarification{
		{
			Identifier: "Foo",
			Question:   "What does Foo do?",
			Answer:     "Foo does the foo thing.",
		},
		{
			Identifier: "Bar",
			Question:   "What does Bar do?",
			Answer:     "Bar does the bar thing.",
		},
	}, calls[0].clarifications)
	require.Equal(t, []docubot.Clarification{
		{
			Identifier: "Foo",
			Question:   "Any extra details?",
			Answer:     "No extra documentation is needed.",
		},
	}, calls[1].clarifications)

	for _, call := range calls {
		require.NotNil(t, call.opts.BaseOptions.Out)
		require.Equal(t, 77, call.opts.BaseOptions.ReflowMaxWidth)
		require.Equal(t, llmmodel.ProviderIDAnthropic.DefaultModel(), call.opts.BaseOptions.Model)
	}
}

func TestRun_DocsImproveFromClarify_DoesNotDeleteFailedRecord(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "p"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "p", "p.go"), []byte("package p\n\nfunc Foo() {}\n"), 0644))
	t.Setenv("CODALOTL_CAS_DB", filepath.Join(tmp, "casdb"))

	origWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	recordPath := filepath.Join(tmp, "record.json")
	require.NoError(t, os.WriteFile(recordPath, []byte("{}"), 0644))

	origFind := findInPlayClarifyRecords
	origImprove := runDocubotImproveFromClarifications
	t.Cleanup(func() {
		findInPlayClarifyRecords = origFind
		runDocubotImproveFromClarifications = origImprove
	})

	findInPlayClarifyRecords = func(*gocas.DB, *gocode.Module) ([]casclarify.InPlayRecord, error) {
		return []casclarify.InPlayRecord{
			{
				Path:          recordPath,
				TargetPackage: "example.com/tmpmod/p",
				Metadata: casclarify.Metadata{Entries: []casclarify.Entry{
					{
						Identifier: "Foo",
						Question:   "What does Foo do?",
						Answer:     "Foo does the foo thing.",
					},
				}},
			},
		}, nil
	}
	runDocubotImproveFromClarifications = func(*gocode.Package, []docubot.Clarification, docubot.ImproveFromClarificationsOptions) ([]docubot.IncorporatedFeedback, error) {
		return nil, errors.New("docubot failed")
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "docs", "improve-from-clarify"}, &RunOptions{Out: &out, Err: &errOut})
	require.Error(t, err)
	require.Equal(t, 1, code)
	require.Empty(t, out.String())
	require.Contains(t, errOut.String(), "docubot failed")

	_, err = os.Stat(recordPath)
	require.NoError(t, err)
}
