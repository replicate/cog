package integration_test

import (
	"fmt"
	"os"
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
		Dir:       "tests",
		Setup:     h.Setup,
		Cmds:      h.Commands(),
		Condition: condition,
	})
}

// condition provides custom conditions for testscript.
// Supported conditions:
//   - slow: marks a test as slow. Use [slow] skip to skip when COG_TEST_FAST=1 is set.
func condition(cond string) (bool, error) {
	switch cond {
	case "slow":
		// slow is true when we should skip slow tests (i.e., when COG_TEST_FAST=1)
		// Usage: [slow] skip 'reason' to skip slow tests in fast mode
		return os.Getenv("COG_TEST_FAST") == "1", nil
	}
	return false, fmt.Errorf("unknown condition: %s", cond)
}
