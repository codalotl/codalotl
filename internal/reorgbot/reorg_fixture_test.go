package reorgbot

import (
	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodetesting"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// withReorgFixture loads files from codeai/reorgbot/testdata into a temp module package using gocodetesting.WithMultiCode.
func withReorgFixture(t *testing.T, f func(*gocode.Package)) {
	testFiles := []string{"app.go", "helpers.go", "app_test.go", "helpers_test.go", "external_test.go"}
	fileToCode := make(map[string]string, len(testFiles))
	for _, filename := range testFiles {
		content, err := os.ReadFile(filepath.Join("testdata", filename))
		if !assert.NoError(t, err) {
			return
		}
		fileToCode[filename] = string(content)
	}
	gocodetesting.WithMultiCode(t, fileToCode, f)
}

// partitionIDs splits identifiers into non-test, main-test, and external-test (black-box) ids.
func partitionIDs(pkg *gocode.Package) (nonTest []string, mainTests []string, extTests []string) {
	opts := gocode.FilterIdentifiersOptions{IncludeTestFuncs: true, IncludeGeneratedFile: false, IncludeAmbiguous: true}
	all := pkg.FilterIdentifiers(nil, opts)
	for _, id := range all {
		s := pkg.GetSnippet(id)
		if s != nil && s.Test() {
			mainTests = append(mainTests, id)
		} else {
			nonTest = append(nonTest, id)
		}
	}
	if pkg.TestPackage != nil {
		extTests = pkg.TestPackage.FilterIdentifiers(nil, opts)
	}
	sort.Strings(nonTest)
	sort.Strings(mainTests)
	sort.Strings(extTests)
	return
}

// jsonOrg returns a simple JSON string mapping a single filename to the given ids.
func jsonOrg(fileName string, ids []string) string {
	var b strings.Builder
	b.WriteString("{\n  \"")
	b.WriteString(fileName)
	b.WriteString("\": [")
	for i, id := range ids {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString("\"")
		b.WriteString(id)
		b.WriteString("\"")
	}
	b.WriteString("]\n}")
	return b.String()
}

// assertSameSource asserts equality of trimmed string. Inequality prints both versions legibly.
func assertSameSource(t *testing.T, want, got string) {
	t.Helper()

	want = strings.TrimSpace(want)
	got = strings.TrimSpace(got)

	if want != got {
		t.Errorf("source not equal.\n\nwant:\n%s\n\ngot:\n%s\n", want, got)
	}
}
