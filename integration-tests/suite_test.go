//go:build integration

package integration_test

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"

	"github.com/replicate/cog/integration-tests/harness"
)

// TestMain sets up signal handling to force exit on cancellation.
// Without this, go test ignores SIGTERM and keeps running when CI cancels.
func TestMain(m *testing.M) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		fmt.Fprintf(os.Stderr, "\nReceived %v, forcing exit...\n", sig)
		os.Exit(1)
	}()

	os.Exit(m.Run())
}

func TestIntegration(t *testing.T) {
	dir := "tests"

	h, err := harness.New()
	if err != nil {
		t.Fatalf("failed to create harness: %v", err)
	}

	files, err := filepath.Glob(filepath.Join(dir, "*.txtar"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), filepath.Ext(f))
		t.Run(name, func(t *testing.T) {
			if !strings.HasSuffix(name, "_serial") {
				t.Parallel()
			}
			testscript.Run(t, testscript.Params{
				Files:     []string{f},
				Setup:     h.Setup,
				Cmds:      h.Commands(),
				Condition: condition,
			})
		})
	}

}

// condition provides custom conditions for testscript.
// Supported conditions:
//   - linux/linux_amd64/amd64: platform guards for specialized tests.
//   - coglet_rust: true when COGLET_WHEEL is set (Rust server configuration)
//
// Note: testscript has built-in support for [short] which checks testing.Short().
func condition(cond string) (bool, error) {
	negated := false
	for strings.HasPrefix(cond, "!") {
		negated = !negated
		cond = cond[1:]
	}

	cogletWheelSet := os.Getenv("COGLET_WHEEL") != ""

	var value bool
	switch cond {
	case "linux":
		value = runtime.GOOS == "linux"
	case "amd64":
		value = runtime.GOARCH == "amd64"
	case "linux_amd64":
		value = runtime.GOOS == "linux" && runtime.GOARCH == "amd64"
	case "coglet_rust":
		value = cogletWheelSet
	default:
		return false, fmt.Errorf("unknown condition: %s", cond)
	}

	if negated {
		value = !value
	}
	return value, nil
}
