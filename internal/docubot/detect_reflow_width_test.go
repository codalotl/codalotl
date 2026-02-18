package docubot

import (
	"testing"

	"github.com/codalotl/codalotl/internal/gocode"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectReflowWidthReflexive(t *testing.T) {
	mod, err := gocode.NewModule(gocode.MustCwd())
	require.NoError(t, err)

	pkg, err := mod.LoadPackageByRelativeDir("internal/docubot")
	require.NoError(t, err)

	width, confident, err := DetectReflowWidth(pkg)
	require.NoError(t, err)
	assert.True(t, confident)
	// This is a reflexive test over this repo: DetectReflowWidth should match our current doc reflow style.
	// If we change our formatting conventions, update this expectation.
	assert.EqualValues(t, 160, width)
}

func TestDetectReflowWidthTestData(t *testing.T) {
	withCodeFixture(t, func(pkg *gocode.Package) {
		width, confident, err := DetectReflowWidth(pkg)
		require.NoError(t, err)

		// The fixture has very few doc lines; we should be not confident and return width 0.
		require.False(t, confident)
		require.Equal(t, 0, width)
	})
}
