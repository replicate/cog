package integration_test

import (
	"fmt"
	"os"
	"runtime"
	"strings"
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
//   - fast: true when COG_TEST_FAST=1 is set. Use [fast] skip to skip slow tests in fast mode.
//   - linux/linux_amd64/amd64: platform guards for specialized tests.
func condition(cond string) (bool, error) {
	negated := false
	for strings.HasPrefix(cond, "!") {
		negated = !negated
		cond = cond[1:]
	}

	var value bool
	switch cond {
	case "fast":
		value = os.Getenv("COG_TEST_FAST") == "1"
	case "linux":
		value = runtime.GOOS == "linux"
	case "amd64":
		value = runtime.GOARCH == "amd64"
	case "linux_amd64":
		value = runtime.GOOS == "linux" && runtime.GOARCH == "amd64"
	default:
		return false, fmt.Errorf("unknown condition: %s", cond)
	}

	if negated {
		value = !value
	}
	return value, nil
}
