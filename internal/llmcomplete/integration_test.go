package llmcomplete

import (
	"os"
	"testing"
)

func runIntegrationTest(t *testing.T, apiKeyEnvVar string) bool {
	t.Helper()

	if apiKeyEnvVar == "" {
		t.Skip("missing api key env var name for this integration test")
		return false
	}
	if os.Getenv(apiKeyEnvVar) == "" {
		t.Skipf("%s is required to run these tests", apiKeyEnvVar)
		return false
	}
	if os.Getenv("INTEGRATION_TEST") != "1" {
		t.Skip("INTEGRATION_TEST=1 is required to run these tests")
		return false
	}
	return true
}
