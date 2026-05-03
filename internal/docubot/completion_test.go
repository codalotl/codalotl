package docubot

import (
	"context"
	"testing"

	"github.com/codalotl/codalotl/internal/llmmodel"
	"github.com/codalotl/codalotl/internal/llmstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type contextErrCompleter struct{}

func (contextErrCompleter) Complete(ctx context.Context, _ llmmodel.ModelID, _, _ string, _ ...llmstream.SendOptions) (llmstream.Turn, error) {
	return llmstream.Turn{}, ctx.Err()
}

func TestCompleteText_UsesOptionsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := completeText("system", "user", BaseOptions{
		Context:   ctx,
		Completer: contextErrCompleter{},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}
