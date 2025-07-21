package testhelpers

import (
	"os"
	"testing"
)

func RequireIntegrationSuite(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1 to run integration tests")
	}
}
