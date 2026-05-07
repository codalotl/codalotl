package docubot

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodecontext"
	"github.com/codalotl/codalotl/internal/gocodetesting"
	"github.com/codalotl/codalotl/internal/q/health"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBaseOptionsUserMessagef_DefaultsToStdout(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	output := captureStdout(t, func() {
		options := BaseOptions{Ctx: health.NewCtx(logger)}
		options.userMessagef("hello %s", "world")
	})

	assert.Equal(t, "hello world\n", output)
	assert.Contains(t, logBuf.String(), "hello world")
}

func TestGenerateAndApplyDocs_UsesInjectedProgressWriter(t *testing.T) {
	originalCode := dedent(`
		func Foo() {}
	`)
	docSnippet := dedentWithBackticks(`
		// Foo does something.
		func Foo()
	`)

	conv := &responsesCompleter{responses: []string{
		"Here are the documentation snippets:\n\n" + docSnippet,
	}}

	gocodetesting.WithCode(t, originalCode, func(pkg *gocode.Package) {
		codeCtx := gocodecontext.NewContext(nil)

		var outBuf bytes.Buffer
		var logBuf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logBuf, nil))

		_, _, err := generateAndApplyDocs(pkg, codeCtx, []string{"Foo"}, false, "", BaseOptions{
			Completer: conv,
			Out:       &outBuf,
			Ctx:       health.NewCtx(logger),
		})
		require.NoError(t, err)

		output := outBuf.String()
		assert.Contains(t, output, "> Requesting docs for 1 identifiers: Foo")
		assert.Contains(t, output, "< Got 1 snippets. 1/1 successful")

		logOutput := logBuf.String()
		assert.Contains(t, logOutput, "> Requesting docs for 1 identifiers: Foo")
		assert.Contains(t, logOutput, "< Got 1 snippets. 1/1 successful")
	})
}

func TestImproveDocs_UsesInjectedProgressWriter(t *testing.T) {
	code := dedent(`
		// Foo does something.
		func Foo() {}
	`)

	improvedSnippet := dedentWithBackticks(`
		// Foo does something better.
		func Foo()
	`)

	conv := &responsesCompleter{responses: []string{
		"Here are the documentation snippets:\n\n" + improvedSnippet,
		`{"Foo":{"best":"A","reason":"Clearer"}}`,
	}}

	gocodetesting.WithCode(t, code, func(pkg *gocode.Package) {
		var outBuf bytes.Buffer

		stdout := captureStdout(t, func() {
			changes, err := ImproveDocs(pkg, []string{"Foo"}, ImproveDocsOptions{
				BaseOptions: BaseOptions{
					Completer: conv,
					Out:       &outBuf,
				},
			})
			require.NoError(t, err)
			require.Len(t, changes, 1)
		})

		assert.Empty(t, stdout)

		output := outBuf.String()
		assert.Contains(t, output, "Improving docs for Foo...")
		assert.Contains(t, output, "> Requesting docs for 1 identifiers: Foo")
		assert.Contains(t, output, "< Got 1 snippets. 1/1 successful")
		assert.Contains(t, output, "New docs for Foo are better. Using...")
	})
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	defer func() {
		os.Stdout = oldStdout
	}()

	fn()

	require.NoError(t, w.Close())

	output, err := io.ReadAll(r)
	require.NoError(t, err)
	require.NoError(t, r.Close())
	return string(output)
}
