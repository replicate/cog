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
//   - linux/linux_amd64/amd64: platform guards for specialized tests.
//   - coglet_alpha: true when COG_WHEEL=coglet-alpha (old Go coglet)
//   - cog_dataclass: true when COG_WHEEL=cog-dataclass (Python 3.10+ only)
//   - cog_rust: true when COG_WHEEL=cog and COGLET_RUST_WHEEL is set
//   - cog_dataclass_rust: true when COG_WHEEL=cog-dataclass and COGLET_RUST_WHEEL is set
//   - coglet_rust: true when COGLET_RUST_WHEEL is set (any Rust server configuration)
//
// Note: testscript has built-in support for [short] which checks testing.Short().
func condition(cond string) (bool, error) {
	negated := false
	for strings.HasPrefix(cond, "!") {
		negated = !negated
		cond = cond[1:]
	}

	cogWheel := os.Getenv("COG_WHEEL")
	rustWheelSet := os.Getenv("COGLET_RUST_WHEEL") != ""

	var value bool
	switch cond {
	case "linux":
		value = runtime.GOOS == "linux"
	case "amd64":
		value = runtime.GOARCH == "amd64"
	case "linux_amd64":
		value = runtime.GOOS == "linux" && runtime.GOARCH == "amd64"
	case "coglet_alpha":
		value = cogWheel == "coglet-alpha"
	case "cog_dataclass":
		value = cogWheel == "cog-dataclass"
	case "cog_rust":
		value = cogWheel == "cog" && rustWheelSet
	case "cog_dataclass_rust":
		value = cogWheel == "cog-dataclass" && rustWheelSet
	case "coglet_rust":
		value = rustWheelSet
	default:
		return false, fmt.Errorf("unknown condition: %s", cond)
	}

	if negated {
		value = !value
	}
	return value, nil
}
