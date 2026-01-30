package health

import (
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHumanError(t *testing.T) {
	err := NewHumanErr("Don't frob that!", "bad_frobbing", "target", "thing")
	assert.Equal(t, "Don't frob that!", err.Error())
	assert.Equal(t, "bad_frobbing[target=thing]", err.(*HumanErr).HealthErr.Error())

	var buf strings.Builder
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	LogErr(logger, err)

	fmt.Println(buf.String())
}
