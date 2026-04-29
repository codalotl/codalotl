package docubot

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codalotl/codalotl/internal/gocode"
	"github.com/codalotl/codalotl/internal/gocodecontext"
	"github.com/codalotl/codalotl/internal/gocodetesting"
	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
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

		_, _, err := generateAndApplyDocs(pkg, codeCtx, []string{"Foo"}, false, BaseOptions{
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

func TestPolish_SerializesConcurrentProgressWrites(t *testing.T) {
	var code strings.Builder
	for i := range 11 {
		_, err := fmt.Fprintf(&code, "// Func%d has docs.\nfunc Func%d() {}\n\n", i, i)
		require.NoError(t, err)
	}

	gocodetesting.WithCode(t, code.String(), func(pkg *gocode.Package) {
		writer := newProgressProbeWriter()

		type result struct {
			changes any
			err     error
		}
		done := make(chan result, 1)
		go func() {
			changes, err := Polish(pkg, nil, PolishOptions{
				BaseOptions: BaseOptions{
					Completer:      echoCompleter{},
					MaxParallelism: 2,
					Out:            writer,
				},
			})
			done <- result{changes: changes, err: err}
		}()

		require.Eventually(t, writer.firstRequestSeen, time.Second, 10*time.Millisecond)
		require.Never(t, writer.secondRequestSeen, 200*time.Millisecond, 10*time.Millisecond)

		close(writer.releaseFirstRequest)

		require.Eventually(t, writer.secondRequestSeen, time.Second, 10*time.Millisecond)

		res := <-done
		require.NoError(t, res.err)
		assert.Empty(t, res.changes)
	})
}

type echoCompleter struct{}

func (echoCompleter) Complete(_ context.Context, _ llmmodel.ModelID, _ string, userMessage string, _ ...llmstream.SendOptions) (llmstream.Turn, error) {
	return llmstream.Turn{
		Role: llmstream.RoleAssistant,
		Parts: []llmstream.ContentPart{
			llmstream.TextContent{Content: userMessage},
		},
		FinishReason: llmstream.FinishReasonEndTurn,
	}, nil
}

type progressProbeWriter struct {
	requestWrites        atomic.Int32
	secondRequestEntered atomic.Bool
	releaseFirstRequest  chan struct{}
	firstRequestEntered  chan struct{}
}

func newProgressProbeWriter() *progressProbeWriter {
	return &progressProbeWriter{
		releaseFirstRequest: make(chan struct{}),
		firstRequestEntered: make(chan struct{}),
	}
}

func (w *progressProbeWriter) Write(p []byte) (int, error) {
	if strings.Contains(string(p), "> Requesting polishing for") {
		switch w.requestWrites.Add(1) {
		case 1:
			close(w.firstRequestEntered)
			<-w.releaseFirstRequest
		case 2:
			w.secondRequestEntered.Store(true)
		}
	}

	return len(p), nil
}

func (w *progressProbeWriter) firstRequestSeen() bool {
	select {
	case <-w.firstRequestEntered:
		return true
	default:
		return false
	}
}

func (w *progressProbeWriter) secondRequestSeen() bool {
	return w.secondRequestEntered.Load()
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
