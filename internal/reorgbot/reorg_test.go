package reorgbot

import (
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodetesting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReorg_AppliesOrganization(t *testing.T) {
	// The mock LLM will return a JSON mapping of filename -> ordered ids.
	withReorgFixture(t, func(pkg *gocode.Package) {
		// Build ids partitioned by phase to mirror Reorg's callbacks.
		nonTest, mainTests, extTests := partitionIDs(pkg)

		// Provide responses for non-tests, internal tests, and external tests.
		half := len(nonTest) / 2
		resp1 := func() string { // non-tests
			var sb strings.Builder
			sb.WriteString("{\n")
			sb.WriteString("  \"app_reorg.go\": [")
			for i, id := range nonTest[:half] {
				if i > 0 {
					sb.WriteString(", ")
				}
				sb.WriteString("\"")
				sb.WriteString(id)
				sb.WriteString("\"")
			}
			sb.WriteString("],\n  \"helpers_reorg.go\": [")
			for i, id := range nonTest[half:] {
				if i > 0 {
					sb.WriteString(", ")
				}
				sb.WriteString("\"")
				sb.WriteString(id)
				sb.WriteString("\"")
			}
			sb.WriteString("]\n}")
			return sb.String()
		}()
		resp2 := func() string { // internal tests in a single file
			return jsonOrg("internal_reorg_test.go", mainTests)
		}()
		resp3 := func() string { // external tests
			return jsonOrg("external_reorg_test.go", extTests)
		}()
		conv := &responsesConversationalist{responses: []string{resp1, resp2, resp3}}

		// Clone the package onto disk to allow file rewrites safely in temp space.
		cloned, err := pkg.Clone()
		require.NoError(t, err)
		defer cloned.Module.DeleteClone()

		// Run Reorg on the cloned package using the stubbed conversationalist.
		err = Reorg(cloned, true, ReorgOptions{BaseOptions: BaseOptions{Conversationalist: conv}})
		require.NoError(t, err)

		// Discover new files by re-reading from disk.
		cloned, err = cloned.Module.ReadPackage(cloned.RelativeDir, nil)
		require.NoError(t, err)

		// Assert non-test outputs exist
		_, okA := cloned.Files["app_reorg.go"]
		_, okB := cloned.Files["helpers_reorg.go"]
		assert.True(t, okA)
		assert.True(t, okB)
		// Ensure original files are removed for non-test
		assert.NotContains(t, cloned.Files, "app.go")
		assert.NotContains(t, cloned.Files, "helpers.go")
		// Internal test files should be reorganized into the new file
		assert.NotContains(t, cloned.Files, "app_test.go")
		assert.NotContains(t, cloned.Files, "helpers_test.go")
		assert.Contains(t, cloned.Files, "internal_reorg_test.go")
		// External test package should be reorganized as well
		if cloned.TestPackage != nil {
			assert.NotContains(t, cloned.TestPackage.Files, "external_test.go")
			assert.Contains(t, cloned.TestPackage.Files, "external_reorg_test.go")
		}
	})
}

func TestReorg_SendsContext(t *testing.T) {
	withReorgFixture(t, func(pkg *gocode.Package) {
		nonTest, mainTests, extTests := partitionIDs(pkg)
		// Provide responses for all phases, including external tests.
		conv := &responsesConversationalist{responses: []string{
			jsonOrg("non_test_file.go", nonTest),
			jsonOrg("main_tests_test.go", mainTests),
			jsonOrg("ext_tests_test.go", extTests),
		}}

		cloned, err := pkg.Clone()
		require.NoError(t, err)
		defer cloned.Module.DeleteClone()
		err = Reorg(cloned, true, ReorgOptions{BaseOptions: BaseOptions{Conversationalist: conv}})
		require.NoError(t, err)

		// Inspect captured user messages
		combined := conv.allUserText()
		// Context should include main package files and key identifiers.
		assert.Contains(t, combined, "// File: app.go")
		assert.Contains(t, combined, "// File: helpers.go")
		assert.Contains(t, combined, "AppConfig")
		assert.Contains(t, combined, "NormalizeName")
		assert.Contains(t, combined, "*App.NumIdleWorkers")
		// External test package context is also generated in its own phase.
		assert.Contains(t, combined, "// File: external_test.go")
	})
}

func TestReorg_Explicit(t *testing.T) {
	withReorgFixture(t, func(pkg *gocode.Package) {
		nonTest, mainTests, extTests := partitionIDs(pkg)

		// Explicit new organization with manual id -> file mapping
		nonTestOrg := map[string][]string{
			"app.go": {
				"App",
				"NewApp",
				"*App.NumIdleWorkers",
				"*App.SetBusy",
				"*App.EachWorker",
				"*App.Name",
			},
			"config.go": {
				"AppConfig",
				"Worker",
			},
			"clamp.go": {
				"Clamp",
			},
			"normalize.go": {
				"NormalizeName",
			},
		}
		mainTestsOrg := map[string][]string{
			"app_test.go": {
				"TestNewAppAndWorkers",
				"TestEachWorkerMutation",
				"TestNormalizeName",
				"TestClamp",
			},
		}
		extTestsOrg := map[string][]string{
			"external_test.go": {
				"TestExternalAPI",
			},
			"clamp_test.go": {
				"TestClamp",
			},
		}

		// Validate coverage equals partitioned ids (order-insensitive)
		flatten := func(m map[string][]string) []string {
			var out []string
			for _, ids := range m {
				out = append(out, ids...)
			}
			sort.Strings(out)
			return out
		}
		expectEqualSets := func(want, got []string) {
			sort.Strings(want)
			sort.Strings(got)
			require.Equal(t, want, got)
		}
		expectEqualSets(nonTest, flatten(nonTestOrg))
		expectEqualSets(mainTests, flatten(mainTestsOrg))
		expectEqualSets(extTests, flatten(extTestsOrg))

		// Prepare deterministic JSON responses preserving id order
		toJSON := func(m map[string][]string) string {
			keys := make([]string, 0, len(m))
			for k := range m {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			var b strings.Builder
			b.WriteString("{\n")
			for i, k := range keys {
				if i > 0 {
					b.WriteString(",\n")
				}
				b.WriteString("  \"")
				b.WriteString(k)
				b.WriteString("\": [")
				ids := m[k]
				for j, id := range ids {
					if j > 0 {
						b.WriteString(", ")
					}
					b.WriteString("\"")
					b.WriteString(id)
					b.WriteString("\"")
				}
				b.WriteString("]")
			}
			b.WriteString("\n}")
			return b.String()
		}
		conv := &responsesConversationalist{responses: []string{
			toJSON(nonTestOrg),
			toJSON(mainTestsOrg),
			toJSON(extTestsOrg),
		}}

		// Clone, run, reload
		cloned, err := pkg.Clone()
		require.NoError(t, err)
		defer cloned.Module.DeleteClone()
		err = Reorg(cloned, true, ReorgOptions{BaseOptions: BaseOptions{Conversationalist: conv}})
		require.NoError(t, err)
		cloned, err = cloned.Module.ReadPackage(cloned.RelativeDir, nil)
		require.NoError(t, err)

		got := string(cloned.Files["app.go"].Contents)
		want := gocodetesting.Dedent(`
			package mypkg

			// App is a small, reorganizable application with workers.
			type App struct {
				cfg     AppConfig
				workers []Worker
			}

			// NewApp constructs a new App with the given configuration and worker count.
			func NewApp(cfg AppConfig) *App {
				if cfg.MaxWorkers <= 0 {
					cfg.MaxWorkers = 1
				}
				workers := make([]Worker, cfg.MaxWorkers)
				for i := 0; i < cfg.MaxWorkers; i++ {
					workers[i] = Worker{ID: i + 1}
				}
				return &App{cfg: cfg, workers: workers}
			}

			// NumIdleWorkers returns the number of workers that are not busy.
			func (a *App) NumIdleWorkers() int {
				idle := 0
				for i := range a.workers {
					if !a.workers[i].Busy {
						idle++
					}
				}
				return idle
			}

			// SetBusy marks a worker busy or idle by id. Returns true if a worker was found.
			func (a *App) SetBusy(workerID int, busy bool) bool {
				for i := range a.workers {
					if a.workers[i].ID == workerID {
						a.workers[i].Busy = busy
						return true
					}
				}
				return false
			}

			// EachWorker applies fn to each worker.
			func (a *App) EachWorker(fn func(w *Worker)) {
				for i := range a.workers {
					fn(&a.workers[i])
				}
			}

			// Name returns the configured application name.
			func (a *App) Name() string { return a.cfg.Name }
		`)
		assertSameSource(t, want, got)

		got = string(cloned.Files["config.go"].Contents)
		want = gocodetesting.Dedent(`
			package mypkg

			// Unattached comment above AppConfig

			// AppConfig holds configuration for the dummy app.
			type AppConfig struct {
				Name       string
				MaxWorkers int
				EnableLogs bool
			}

			// Worker represents a unit that can process tasks.
			type Worker struct {
				ID   int
				Busy bool
			}
		`)
		assertSameSource(t, want, got)

		got = string(cloned.Files["clamp.go"].Contents)
		want = gocodetesting.Dedent(`
			package mypkg

			// Clamp clamps n between min and max inclusive.
			func Clamp(n, min, max int) int {
				if min > max {
					min, max = max, min
				}
				if n < min {
					return min
				}
				if n > max {
					return max
				}
				return n
			}
		`)
		assertSameSource(t, want, got)

		got = string(cloned.Files["normalize.go"].Contents)
		want = gocodetesting.Dedent(`
			package mypkg

			import "strings"

			// NormalizeName trims whitespace and title-cases the app or worker name.
			func NormalizeName(s string) string {
				s = strings.TrimSpace(s)
				if s == "" {
					return s
				}
				parts := strings.Fields(s)
				for i := range parts {
					if len(parts[i]) == 1 {
						parts[i] = strings.ToUpper(parts[i])
						continue
					}
					parts[i] = strings.ToUpper(parts[i][:1]) + strings.ToLower(parts[i][1:])
				}
				return strings.Join(parts, " ")
			}
		`)
		assertSameSource(t, want, got)

		// internal_explicit_test.go
		got = string(cloned.Files["app_test.go"].Contents)
		want = gocodetesting.Dedent(`
			package mypkg

			import (
				"testing"

				"github.com/stretchr/testify/require"
			)

			func TestNewAppAndWorkers(t *testing.T) {
				cfg := AppConfig{Name: "demo app", MaxWorkers: 3, EnableLogs: true}
				a := NewApp(cfg)
				require.Equal(t, "demo app", a.Name())
				require.Equal(t, 3, a.NumIdleWorkers())

				// Flip a worker to busy and verify counts change
				ok := a.SetBusy(2, true)
				require.True(t, ok)
				require.Equal(t, 2, a.NumIdleWorkers())
			}

			func TestEachWorkerMutation(t *testing.T) {
				a := NewApp(AppConfig{Name: "t", MaxWorkers: 2})
				a.EachWorker(func(w *Worker) { w.Busy = true })
				require.Equal(t, 0, a.NumIdleWorkers())
			}

			func TestNormalizeName(t *testing.T) {
				require.Equal(t, "Hello World", NormalizeName("  hello   world  "))
				require.Equal(t, "A", NormalizeName("a"))
				require.Equal(t, "", NormalizeName("   "))
			}

			func TestClamp(t *testing.T) {
				require.Equal(t, 5, Clamp(5, 1, 10))
				require.Equal(t, 1, Clamp(-100, 1, 10))
				require.Equal(t, 10, Clamp(100, 1, 10))
				// reversed min/max should still work
				require.Equal(t, 7, Clamp(7, 10, 1))
			}
		`)
		assertSameSource(t, want, got)

		require.NotNil(t, cloned.TestPackage)
		got = string(cloned.TestPackage.Files["external_test.go"].Contents)
		want = gocodetesting.Dedent(`
			package mypkg_test

			import (
				"testing"

				"github.com/stretchr/testify/require"

				reorgapp "github.com/codalotl/codalotl/internal/reorgbot/testdata"
			)

			func TestExternalAPI(t *testing.T) {
				cfg := reorgapp.AppConfig{Name: " ext name ", MaxWorkers: 2}
				a := reorgapp.NewApp(cfg)

				// external users can call exported methods and functions only
				require.Equal(t, 2, a.NumIdleWorkers())

				name := reorgapp.NormalizeName(" ext app ")
				require.Equal(t, "Ext App", name)
			}
		`)
		assertSameSource(t, want, got)

		got = string(cloned.TestPackage.Files["clamp_test.go"].Contents)
		want = gocodetesting.Dedent(`
			package mypkg_test

			import (
				"testing"

				reorgapp "github.com/codalotl/codalotl/internal/reorgbot/testdata"
				"github.com/stretchr/testify/require"
			)

			func TestClamp(t *testing.T) {
				require.Equal(t, 5, reorgapp.Clamp(5, 1, 10))
			}
		`)
		assertSameSource(t, want, got)
	})
}

func TestReorg_EmbedAndSideEffectImports(t *testing.T) {
	// Build a minimal package with an embed directive (no embed import) and a side-effect import in another file.
	gocodetesting.WithMultiCode(t, map[string]string{
		"emb.go": gocodetesting.Dedent(`
            package mypkg

            //go:embed assets.txt
            var data string

            func Foo() {}
        `),
		"pprof.go": gocodetesting.Dedent(`
            package mypkg

            import _ "net/http/pprof"

            func Bar() {}
        `),
		"assets.txt": "hello\n",
	}, func(pkg *gocode.Package) {
		// Collect ids for non-tests only
		ids := pkg.FilterIdentifiers(nil, gocode.FilterIdentifiersOptions{IncludeTestFuncs: true, IncludeGeneratedFile: false, IncludeAmbiguous: true})

		// Arrange the LLM to put all non-test ids into a single file so that
		// embed and side-effect behaviors can be verified together.
		conv := &responsesConversationalist{responses: []string{
			jsonOrg("reorg.go", ids),
		}}

		// Clone, run, reload
		cloned, err := pkg.Clone()
		require.NoError(t, err)
		defer cloned.Module.DeleteClone()

		err = Reorg(cloned, true, ReorgOptions{BaseOptions: BaseOptions{Conversationalist: conv}})
		require.NoError(t, err)

		cloned, err = cloned.Module.ReadPackage(cloned.RelativeDir, nil)
		require.NoError(t, err)

		// Verify the resulting file contains the embed directive and that `_ "embed"` was added,
		// and that the pre-existing side-effect import is still present in some file.
		var found bool
		for name, f := range cloned.Files {
			if name != "reorg.go" {
				continue
			}
			src := string(f.Contents)
			assert.Contains(t, src, "//go:embed")
			assert.Contains(t, src, "_ \"embed\"")
			assert.Contains(t, src, "_ \"net/http/pprof\"")
			found = true
		}
		assert.True(t, found, "reorg.go not found")
	})
}

func TestReorg_TwoPhase(t *testing.T) {
	withReorgFixture(t, func(pkg *gocode.Package) {
		nonTest, mainTests, extTests := partitionIDs(pkg)

		// Deterministic split: app-centric vs helper-centric ids
		var appGroup, helperGroup []string
		for _, id := range nonTest {
			if strings.Contains(id, "NormalizeName") || strings.Contains(id, "Clamp") {
				helperGroup = append(helperGroup, id)
			} else {
				appGroup = append(appGroup, id)
			}
		}

		// Organization phase JSON
		respOrgNon := func() string {
			var sb strings.Builder
			sb.WriteString("{\n")
			sb.WriteString("  \"app_reorg.go\": [")
			for i, id := range appGroup {
				if i > 0 {
					sb.WriteString(", ")
				}
				sb.WriteString("\"")
				sb.WriteString(id)
				sb.WriteString("\"")
			}
			sb.WriteString("],\n  \"helpers_reorg.go\": [")
			for i, id := range helperGroup {
				if i > 0 {
					sb.WriteString(", ")
				}
				sb.WriteString("\"")
				sb.WriteString(id)
				sb.WriteString("\"")
			}
			sb.WriteString("]\n}")
			return sb.String()
		}()
		respOrgMain := jsonOrg("internal_reorg_test.go", mainTests)
		respOrgExt := jsonOrg("external_reorg_test.go", extTests)

		// stagedConversationalist: 3 initial org responses, then dynamic arrays for resort
		conv := &stagedConversationalist{initial: []string{respOrgNon, respOrgMain, respOrgExt}}

		// Clone, run with oneShot=false (two-phase), then reload
		cloned, err := pkg.Clone()
		require.NoError(t, err)
		defer cloned.Module.DeleteClone()

		err = Reorg(cloned, false, ReorgOptions{BaseOptions: BaseOptions{Conversationalist: conv}})
		require.NoError(t, err)

		cloned, err = cloned.Module.ReadPackage(cloned.RelativeDir, nil)
		require.NoError(t, err)

		// Verify reorganized files exist and originals are gone
		_, okA := cloned.Files["app_reorg.go"]
		_, okB := cloned.Files["helpers_reorg.go"]
		assert.True(t, okA)
		assert.True(t, okB)
		assert.NotContains(t, cloned.Files, "app.go")
		assert.NotContains(t, cloned.Files, "helpers.go")

		// Internal tests reorganized
		assert.Contains(t, cloned.Files, "internal_reorg_test.go")
		assert.NotContains(t, cloned.Files, "app_test.go")
		assert.NotContains(t, cloned.Files, "helpers_test.go")

		// External tests reorganized
		if cloned.TestPackage != nil {
			assert.Contains(t, cloned.TestPackage.Files, "external_reorg_test.go")
			assert.NotContains(t, cloned.TestPackage.Files, "external_test.go")
		}
	})
}

func TestResortFile_NoDuplicatePreCommentsFromPreservedPrefix(t *testing.T) {
	// Scenario: a large unattached comment originally above package is moved by reorg
	// to live between package and first decl. ResortFile should preserve it in the prefix
	// and must NOT emit it again as a pre-comment for the first snippet.
	gocodetesting.WithMultiCode(t, map[string]string{
		"a.go": gocodetesting.Dedent(`
            // big unattached top block line 1
            // big unattached top block line 2

            package mypkg

            // Foo doc
            type Foo int
        `),
	}, func(pkg *gocode.Package) {
		// Emulate a prior reorg that kept everything in the same file but would
		// cause the top comments to be considered pre-comments to Foo (not AbovePackage)
		// by writing a file where those comments already sit between package and Foo.
		// We simulate by creating such a file content explicitly, then calling ResortFile.

		// Build id map and pick Foo id
		ids := pkg.Identifiers(true)
		_, idToSnippet := codeContextForPackage(pkg, ids, false)
		var fooID string
		for id, sn := range idToSnippet {
			if ts, ok := sn.(*gocode.TypeSnippet); ok {
				if len(ts.Identifiers) == 1 && ts.Identifiers[0] == "Foo" {
					fooID = id
					break
				}
			}
		}
		require.NotEmpty(t, fooID)

		// Manually rewrite a.go to place the unattached block between package and Foo,
		// mimicking a state where the block is inside the preserved prefix.
		path := pkg.Module.AbsolutePath + "/" + pkg.RelativeDir + "/a.go"
		content := gocodetesting.Dedent(`
            package mypkg

            // big unattached top block line 1
            // big unattached top block line 2

            // Foo doc
            type Foo int
        `)
		err := os.WriteFile(path, []byte(content), 0644)
		require.NoError(t, err)

		// Reload package so ResortFile sees the new layout
		pkg, err = pkg.Module.ReadPackage(pkg.RelativeDir, nil)
		require.NoError(t, err)

		// Ask ResortFile to emit same order (single snippet). It should not duplicate the block.
		resp := "[\"" + fooID + "\"]"
		conv := &responsesConversationalist{responses: []string{resp}}
		err = ResortFile(pkg, "a.go", ReorgOptions{BaseOptions: BaseOptions{Conversationalist: conv}})
		require.NoError(t, err)

		// Reload and validate that the big block appears exactly once, above Foo, not twice.
		pkg, err = pkg.Module.ReadPackage(pkg.RelativeDir, nil)
		require.NoError(t, err)
		got := string(pkg.Files["a.go"].Contents)
		// Count occurrences of a distinctive line
		count := strings.Count(got, "big unattached top block line 1")
		assert.Equal(t, 1, count, "unattached block should not be duplicated")
		// Ensure order is: package, block, then Foo decl
		assert.Less(t, strings.Index(got, "big unattached top block line 1"), strings.Index(got, "type Foo int"))
	})
}
