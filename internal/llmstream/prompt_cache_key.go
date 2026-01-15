package llmstream

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"io"

	"github.com/codalotl/codalotl/internal/llmmodel"
)

func computePromptCacheKey(modelID llmmodel.ModelID, systemMessage string) string {
	key, err := newPromptCacheKeyFromReader(rand.Reader)
	if err == nil {
		return key
	}
	return ""
}

func newPromptCacheKeyFromReader(r io.Reader) (string, error) {
	// OpenAI expects an opaque string. We'll generate a random 32-byte value and
	// hash it to ensure fixed size and uniform hex output.
	b := make([]byte, 32)
	if _, err := io.ReadFull(r, b); err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
