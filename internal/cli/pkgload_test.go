package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/codalotl/codalotl/internal/noninteractive"
	qcli "github.com/codalotl/codalotl/internal/q/cli"
	"github.com/stretchr/testify/require"
)

func TestLoadPackageArg_ImportPathsTakePrecedenceOverFallbackDirs(t *testing.T) {
	tmp := mkdirTempWithRemoveRetry(t, "codalotl-cli-pkgload-")
	writePkgLoadModule(t, tmp, `module example.com/tmpmod

go 1.22

require example.com/dep v0.0.0

replace example.com/dep => ./depmod
`)
	writePkgLoadPackage(t, filepath.Join(tmp, "fmt"), "fmt")
	writePkgLoadPackage(t, filepath.Join(tmp, "p"), "p")
	writePkgLoadPackage(t, filepath.Join(tmp, "example.com", "tmpmod", "p"), "shadowp")

	depMod := filepath.Join(tmp, "depmod")
	writePkgLoadModule(t, depMod, "module example.com/dep\n\ngo 1.22\n")
	writePkgLoadPackage(t, filepath.Join(depMod, "pkg"), "pkg")
	writePkgLoadPackage(t, filepath.Join(tmp, "example.com", "dep", "pkg"), "shadowdep")

	chdirForTest(t, tmp)

	pkg, _, err := loadPackageArg("fmt")
	require.NoError(t, err)
	require.Equal(t, "fmt", pkg.ImportPath)
	requireNotSameDir(t, filepath.Join(tmp, "fmt"), pkg.AbsolutePath())

	pkg, _, err = loadPackageArg("./fmt")
	require.NoError(t, err)
	require.Equal(t, "example.com/tmpmod/fmt", pkg.ImportPath)
	requireSameDir(t, filepath.Join(tmp, "fmt"), pkg.AbsolutePath())

	pkg, _, err = loadPackageArg("example.com/tmpmod/p/")
	require.NoError(t, err)
	require.Equal(t, "example.com/tmpmod/p", pkg.ImportPath)
	requireSameDir(t, filepath.Join(tmp, "p"), pkg.AbsolutePath())

	pkg, _, err = loadPackageArg("./example.com/tmpmod/p")
	require.NoError(t, err)
	require.Equal(t, "example.com/tmpmod/example.com/tmpmod/p", pkg.ImportPath)
	requireSameDir(t, filepath.Join(tmp, "example.com", "tmpmod", "p"), pkg.AbsolutePath())

	pkg, _, err = loadPackageArg("example.com/dep/pkg")
	require.NoError(t, err)
	require.Equal(t, "example.com/dep/pkg", pkg.ImportPath)
	requireSameDir(t, filepath.Join(depMod, "pkg"), pkg.AbsolutePath())

	pkg, _, err = loadPackageArg("./example.com/dep/pkg")
	require.NoError(t, err)
	require.Equal(t, "example.com/tmpmod/example.com/dep/pkg", pkg.ImportPath)
	requireSameDir(t, filepath.Join(tmp, "example.com", "dep", "pkg"), pkg.AbsolutePath())
}

func TestLoadPackageArg_ResolvesImportPathOutsideModule(t *testing.T) {
	tmp := mkdirTempWithRemoveRetry(t, "codalotl-cli-pkgload-")

	withLocal := filepath.Join(tmp, "with-local")
	writePkgLoadPackage(t, filepath.Join(withLocal, "fmt"), "fmt")
	chdirForTest(t, withLocal)

	pkg, _, err := loadPackageArg("fmt")
	require.NoError(t, err)
	require.Equal(t, "fmt", pkg.ImportPath)
	requireNotSameDir(t, filepath.Join(withLocal, "fmt"), pkg.AbsolutePath())

	withoutLocal := filepath.Join(tmp, "without-local")
	require.NoError(t, os.MkdirAll(withoutLocal, 0755))
	chdirForTest(t, withoutLocal)

	pkg, _, err = loadPackageArg("fmt")
	require.NoError(t, err)
	require.Equal(t, "fmt", pkg.ImportPath)
}

func TestLoadPackageArg_AcceptsExplicitAndFallbackDirs(t *testing.T) {
	tmp := mkdirTempWithRemoveRetry(t, "codalotl-cli-pkgload-")
	writePkgLoadModule(t, tmp, "module example.com/forms\n\ngo 1.22\n")
	writePkgLoadPackage(t, tmp, "root")
	writePkgLoadPackage(t, filepath.Join(tmp, "nested"), "nested")
	writePkgLoadPackage(t, filepath.Join(tmp, "nested", "local"), "local")
	writePkgLoadPackage(t, filepath.Join(tmp, "foo"), "foo")

	chdirForTest(t, filepath.Join(tmp, "nested"))

	tests := []struct {
		arg        string
		wantImport string
		wantDir    string
	}{
		{arg: ".", wantImport: "example.com/forms/nested", wantDir: filepath.Join(tmp, "nested")},
		{arg: "..", wantImport: "example.com/forms", wantDir: tmp},
		{arg: "./local", wantImport: "example.com/forms/nested/local", wantDir: filepath.Join(tmp, "nested", "local")},
		{arg: "../foo", wantImport: "example.com/forms/foo", wantDir: filepath.Join(tmp, "foo")},
		{arg: filepath.Join(tmp, "foo"), wantImport: "example.com/forms/foo", wantDir: filepath.Join(tmp, "foo")},
		{arg: "local", wantImport: "example.com/forms/nested/local", wantDir: filepath.Join(tmp, "nested", "local")},
	}

	for _, tt := range tests {
		t.Run(tt.arg, func(t *testing.T) {
			pkg, _, err := loadPackageArg(tt.arg)
			require.NoError(t, err)
			require.Equal(t, tt.wantImport, pkg.ImportPath)
			requireSameDir(t, tt.wantDir, pkg.AbsolutePath())
		})
	}
}

func TestLoadPackageArg_ModuleRootImportPathHasEmptyRelativeDir(t *testing.T) {
	tmp := mkdirTempWithRemoveRetry(t, "codalotl-cli-pkgload-")
	writePkgLoadModule(t, tmp, "module example.com/root\n\ngo 1.22\n")
	writePkgLoadPackage(t, tmp, "root")
	chdirForTest(t, tmp)

	pkg, _, err := loadPackageArg("example.com/root")
	require.NoError(t, err)
	require.Equal(t, "example.com/root", pkg.ImportPath)
	require.Empty(t, pkg.RelativeDir)
	requireSameDir(t, tmp, pkg.AbsolutePath())
}

func TestLoadPackageArg_FallbackDirMayBeInModuleBelowCWD(t *testing.T) {
	tmp := mkdirTempWithRemoveRetry(t, "codalotl-cli-pkgload-")
	proj := filepath.Join(tmp, "proj")
	pkgDir := filepath.Join(proj, "internal", "cli")
	writePkgLoadModule(t, proj, "module example.com/proj\n\ngo 1.22\n")
	writePkgLoadPackage(t, pkgDir, "cli")

	chdirForTest(t, tmp)

	for _, arg := range []string{
		filepath.Join("proj", "internal", "cli"),
		"." + string(filepath.Separator) + filepath.Join("proj", "internal", "cli"),
		pkgDir,
	} {
		t.Run(arg, func(t *testing.T) {
			pkg, _, err := loadPackageArg(arg)
			require.NoError(t, err)
			require.Equal(t, "example.com/proj/internal/cli", pkg.ImportPath)
			requireSameDir(t, pkgDir, pkg.AbsolutePath())
		})
	}
}

func TestLoadPackageArg_ReturnsMalformedModuleErrorsForDirs(t *testing.T) {
	tmp := mkdirTempWithRemoveRetry(t, "codalotl-cli-pkgload-")
	badMod := filepath.Join(tmp, "badmod")
	pkgDir := filepath.Join(badMod, "pkg")
	writePkgLoadModule(t, badMod, "not a go.mod\n")
	writePkgLoadPackage(t, pkgDir, "pkg")
	chdirForTest(t, tmp)

	tests := []string{
		filepath.Join("badmod", "pkg"),
		"." + string(filepath.Separator) + filepath.Join("badmod", "pkg"),
		pkgDir,
	}
	for _, arg := range tests {
		t.Run(arg, func(t *testing.T) {
			pkg, mod, err := loadPackageArg(arg)
			require.Error(t, err)
			require.Nil(t, pkg)
			require.Nil(t, mod)
			require.Contains(t, err.Error(), "module")

			var usageErr qcli.UsageError
			require.NotErrorAs(t, err, &usageErr)
		})
	}
}

func TestLoadPackageArg_RejectsPackagePatterns(t *testing.T) {
	_, _, err := loadPackageArg("./...")
	require.Error(t, err)

	var usageErr qcli.UsageError
	require.ErrorAs(t, err, &usageErr)
}

func TestResolveSpecPathArg_ImportPathPrecedesBareDir(t *testing.T) {
	tmp := mkdirTempWithRemoveRetry(t, "codalotl-cli-pkgload-")
	writePkgLoadModule(t, tmp, "module example.com/tmpmod\n\ngo 1.22\n")
	writePkgLoadPackage(t, filepath.Join(tmp, "p"), "p")
	writePkgLoadPackage(t, filepath.Join(tmp, "example.com", "tmpmod", "p"), "shadowp")
	writePkgLoadSpec(t, filepath.Join(tmp, "p"), "# p\n")
	writePkgLoadSpec(t, filepath.Join(tmp, "example.com", "tmpmod", "p"), "# shadow\n")

	chdirForTest(t, tmp)

	got, err := resolveSpecPathArg("example.com/tmpmod/p")
	require.NoError(t, err)
	requireSamePath(t, filepath.Join(tmp, "p", "SPEC.md"), got)

	got, err = resolveSpecPathArg("./example.com/tmpmod/p")
	require.NoError(t, err)
	requireSamePath(t, filepath.Join(tmp, "example.com", "tmpmod", "p", "SPEC.md"), got)
}

func TestRun_ExecPackage_ResolvesCLIImportPath(t *testing.T) {
	isolateUserConfig(t)

	tmp := mkdirTempWithRemoveRetry(t, "codalotl-cli-pkgload-")
	writePkgLoadModule(t, tmp, "module example.com/tmpmod\n\ngo 1.22\n")
	writePkgLoadPackage(t, filepath.Join(tmp, "p"), "p")
	chdirForTest(t, tmp)

	origRunNoninteractiveExec := runNoninteractiveExec
	t.Cleanup(func() { runNoninteractiveExec = origRunNoninteractiveExec })

	var gotOpts noninteractive.Options
	runNoninteractiveExec = func(_ string, opts noninteractive.Options) error {
		gotOpts = opts
		return nil
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code, err := Run([]string{"codalotl", "exec", "--package", "example.com/tmpmod/p", "hello"}, &RunOptions{Out: &out, Err: &errOut})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Empty(t, errOut.String())
	requireSameDir(t, filepath.Join(tmp, "p"), gotOpts.PackagePath)
}

func writePkgLoadModule(t *testing.T, dir string, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(body), 0644))
}

func writePkgLoadPackage(t *testing.T, dir string, packageName string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, packageName+".go"), []byte("package "+packageName+"\n\nfunc F() {}\n"), 0644))
}

func writePkgLoadSpec(t *testing.T, dir string, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SPEC.md"), []byte(body), 0644))
}

func requireSameDir(t *testing.T, want string, got string) {
	t.Helper()
	wantPath, err := filepath.EvalSymlinks(want)
	require.NoError(t, err)
	gotPath, err := filepath.EvalSymlinks(got)
	require.NoError(t, err)
	require.Equal(t, wantPath, gotPath)
}

func requireNotSameDir(t *testing.T, notWant string, got string) {
	t.Helper()
	notWantPath, err := filepath.EvalSymlinks(notWant)
	require.NoError(t, err)
	gotPath, err := filepath.EvalSymlinks(got)
	require.NoError(t, err)
	require.NotEqual(t, notWantPath, gotPath)
}

func requireSamePath(t *testing.T, want string, got string) {
	t.Helper()
	wantAbs, err := filepath.Abs(want)
	require.NoError(t, err)
	gotAbs, err := filepath.Abs(got)
	require.NoError(t, err)
	wantPath, err := filepath.EvalSymlinks(wantAbs)
	require.NoError(t, err)
	gotPath, err := filepath.EvalSymlinks(gotAbs)
	require.NoError(t, err)
	require.Equal(t, filepath.Clean(wantPath), filepath.Clean(gotPath))
}
