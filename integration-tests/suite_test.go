package integration_test

import (
	"testing"

	"github.com/rogpeppe/go-internal/testscript"

	"github.com/replicate/cog/integration-tests/harness"
)

func TestIntegration(t *testing.T) {
	h, err := harness.New()
	if err != nil {
		t.Fatalf("failed to create harness: %v", err)
	}

	testscript.Run(t, testscript.Params{
		Dir:   "tests",
		Setup: h.Setup,
		Cmds:  h.Commands(),
	})
}
