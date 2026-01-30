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
