package gousage

import (
	"path/filepath"
	"testing"

	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodetesting"
	"github.com/stretchr/testify/require"
)

func TestUsedBy(t *testing.T) {
	gocodetesting.WithMultiCode(t, map[string]string{
		"placeholder.go": gocodetesting.Dedent(`
			package mypkg

			const sentinel = "placeholder"
		`),
	}, func(pkg *gocode.Package) {
		mod := pkg.Module
		require.NotNil(t, mod)

		err := gocodetesting.AddPackage(t, mod, "target", map[string]string{
			"target.go": gocodetesting.Dedent(`
				package target

				const Name = "target"
			`),
		})
		require.NoError(t, err)

		err = gocodetesting.AddPackage(t, mod, "consumer1", map[string]string{
			"consumer1.go": gocodetesting.Dedent(`
				package consumer1

				import "mymodule/target"

				var _ = target.Name
			`),
			"consumer1_test.go": gocodetesting.Dedent(`
				package consumer1_test

				import (
					"mymodule/target"
					"testing"
				)

				func TestUse(t *testing.T) {
					_ = target.Name
				}
			`),
		})
		require.NoError(t, err)

		err = gocodetesting.AddPackage(t, mod, "consumer2", map[string]string{
			"consumer2.go": gocodetesting.Dedent(`
				package consumer2

				import "mymodule/target"

				var _ = target.Name
			`),
		})
		require.NoError(t, err)

		targetPkg, err := mod.LoadPackageByRelativeDir("target")
		require.NoError(t, err)

		usages, err := UsedBy(targetPkg)
		require.NoError(t, err)

		dir := mod.AbsolutePath
		want := []Usage{
			{
				ImportPath:   "mymodule/consumer1",
				AbsolutePath: filepath.Join(dir, "consumer1"),
				RelativePath: "consumer1",
			},
			{
				ImportPath:   "mymodule/consumer1_test",
				AbsolutePath: filepath.Join(dir, "consumer1"),
				RelativePath: "consumer1",
			},
			{
				ImportPath:   "mymodule/consumer2",
				AbsolutePath: filepath.Join(dir, "consumer2"),
				RelativePath: "consumer2",
			},
		}

		require.Equal(t, want, usages)
	})
}

func TestUsedByErrors(t *testing.T) {
	_, err := UsedBy(nil)
	require.Error(t, err, "UsedBy(nil)")

	pkg := &gocode.Package{ImportPath: "example.com/testmodule/solo"}
	_, err = UsedBy(pkg)
	require.Error(t, err, "UsedBy without module")
}
