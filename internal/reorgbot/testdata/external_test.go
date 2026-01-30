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

func TestClamp(t *testing.T) {
	require.Equal(t, 5, reorgapp.Clamp(5, 1, 10))
}
