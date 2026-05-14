package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codalotl/codalotl/internal/docubot"
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/stretchr/testify/require"
)

func TestRun_DocsFix_UsesDocubotWritesCASAndSummary(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()
	originalSource := "package p\n\n// Foo is wrong.\nfunc Foo() {}\n"
	fixedSource := "package p\n\n// Foo is correct now.\nfunc Foo() {}\n"
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, ".codalotl"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, ".codalotl", "config.json"), []byte(`{
  "preferredprovider": "anthropic",
  "reflowwidth": 77
}`), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "p"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "p", "p.go"), []byte(originalSource), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "p", "p_test.go"), []byte("package p\n\n// Helper is documented.\nfunc Helper() {}\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "p", "external_test.go"), []byte("package p_test\n\n// ExternalHelper is documented.\nfunc ExternalHelper() {}\n"), 0644))

	t.Setenv("CODALOTL_CAS_DB", filepath.Join(tmp, "casdb"))
	chdirForTest(t, tmp)

	origRun := runDocubotFindAndFixDocErrors
	t.Cleanup(func() { runDocubotFindAndFixDocErrors = origRun })

	var gotPkg *gocode.Package
	var gotIdentifiers []string
	var gotOpts docubot.FindFixDocErrorsOptions
	runDocubotFindAndFixDocErrors = func(pkg *gocode.Package, identifiers []string, opts docubot.FindFixDocErrorsOptions) ([]docubot.IncorporatedFeedback, error) {
		gotPkg = pkg
		gotIdentifiers = append([]string(nil), identifiers...)
		gotOpts = opts
		_, err := opts.BaseOptions.Out.Write([]byte("Checking docs...\n"))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(pkg.AbsolutePath(), "p.go"), []byte(fixedSource), 0644))
		return []docubot.IncorporatedFeedback{{}, {}}, nil
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "docs", "fix", "example.com/tmpmod/p"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())

	require.NotNil(t, gotPkg)
	require.Equal(t, "example.com/tmpmod/p", gotPkg.ImportPath)
	require.True(t, gotPkg.HasTestPackage())
	require.Empty(t, gotIdentifiers)
	require.Equal(t, 77, gotOpts.BaseOptions.ReflowMaxWidth)
	require.Equal(t, llmmodel.ProviderIDAnthropic.DefaultModel(), gotOpts.BaseOptions.Model)

	gotOut := out.String()
	require.Contains(t, gotOut, "Checking docs...\n")
	require.Contains(t, gotOut, "Applied 2 documentation fix(es).\n")
	result := requireDocsFixResultLine(t, gotOut)
	require.Equal(t, 2, result.FixCount)
	require.Equal(t, string(docsFixCASNamespace), result.CASNamespace)
	require.Equal(t, docsFixModeWholePackage, result.Mode)
	require.Empty(t, result.Identifiers)
	require.NotEmpty(t, result.CASRecordPath)
	require.Contains(t, result.CASRecordPath, string(docsFixCASNamespace))
	_, err = os.Stat(result.CASRecordPath)
	require.NoError(t, err)

	mod, err := gocode.NewModule(tmp)
	require.NoError(t, err)
	pkg, err := mod.LoadPackageByRelativeDir("p")
	require.NoError(t, err)
	db, err := casReadDBForBaseDir(tmp)
	require.NoError(t, err)
	var value docsFixCASValue
	ok, _, err := db.RetrieveOnPackage(pkg, docsFixCASNamespace, &value)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, string(docsFixCASNamespace), value.Schema)
	require.Equal(t, docsFixModeWholePackage, value.Mode)
	require.Empty(t, value.Identifiers)
	require.Equal(t, 2, value.FixCount)

	require.NoError(t, os.WriteFile(filepath.Join(tmp, "p", "p.go"), []byte(originalSource), 0644))
	mod, err = gocode.NewModule(tmp)
	require.NoError(t, err)
	pkg, err = mod.LoadPackageByRelativeDir("p")
	require.NoError(t, err)
	ok, _, err = db.RetrieveOnPackage(pkg, docsFixCASNamespace, &value)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestRun_DocsFix_IdentifierLimitedCASRecordIsMarked(t *testing.T) {
	isolateUserConfig(t)

	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "go.mod"), []byte("module example.com/tmpmod\n\ngo 1.22\n"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "p"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "p", "p.go"), []byte("package p\n\n// Foo is documented.\nfunc Foo() {}\n\n// Bar is documented.\nfunc Bar() {}\n"), 0644))

	t.Setenv("CODALOTL_CAS_DB", filepath.Join(tmp, "casdb"))
	chdirForTest(t, tmp)

	origRun := runDocubotFindAndFixDocErrors
	t.Cleanup(func() { runDocubotFindAndFixDocErrors = origRun })

	var gotIdentifiers []string
	runDocubotFindAndFixDocErrors = func(_ *gocode.Package, identifiers []string, _ docubot.FindFixDocErrorsOptions) ([]docubot.IncorporatedFeedback, error) {
		gotIdentifiers = append([]string(nil), identifiers...)
		return nil, nil
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "docs", "fix", "--identifiers", "Bar, Foo,Bar", "./p"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())
	require.Equal(t, []string{"Bar", "Foo"}, gotIdentifiers)

	result := requireDocsFixResultLine(t, out.String())
	require.Equal(t, 0, result.FixCount)
	require.Equal(t, docsFixModeIdentifiers, result.Mode)
	require.Equal(t, []string{"Bar", "Foo"}, result.Identifiers)

	mod, err := gocode.NewModule(tmp)
	require.NoError(t, err)
	pkg, err := mod.LoadPackageByRelativeDir("p")
	require.NoError(t, err)
	db, err := casReadDBForBaseDir(tmp)
	require.NoError(t, err)
	var value docsFixCASValue
	ok, _, err := db.RetrieveOnPackage(pkg, docsFixCASNamespace, &value)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, docsFixModeIdentifiers, value.Mode)
	require.Equal(t, []string{"Bar", "Foo"}, value.Identifiers)
}

func TestParseDocsFixIdentifiers(t *testing.T) {
	got, err := parseDocsFixIdentifiers(" Foo,Bar, Foo ")
	require.NoError(t, err)
	require.Equal(t, []string{"Bar", "Foo"}, got)

	got, err = parseDocsFixIdentifiers("")
	require.NoError(t, err)
	require.Empty(t, got)

	_, err = parseDocsFixIdentifiers("Foo,,Bar")
	require.Error(t, err)
}

func requireDocsFixResultLine(t *testing.T, stdout string) docsFixSummary {
	t.Helper()

	for _, line := range strings.Split(stdout, "\n") {
		if !strings.HasPrefix(line, docsFixResultLinePrefix) {
			continue
		}
		var result docsFixSummary
		require.NoError(t, json.Unmarshal([]byte(strings.TrimPrefix(line, docsFixResultLinePrefix)), &result))
		return result
	}
	t.Fatalf("expected %s line in stdout:\n%s", docsFixResultLinePrefix, stdout)
	return docsFixSummary{}
}
