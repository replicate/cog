package integration_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"

	"github.com/replicate/cog/integration-tests/harness"
)

func TestIntegration(t *testing.T) {
	h, err := harness.New()
	if err != nil {
		t.Fatalf("failed to create harness: %v", err)
	}

	// Find all fixtures
	fixturesDir := "fixtures"
	fixtures, err := os.ReadDir(fixturesDir)
	if err != nil {
		t.Fatalf("failed to read fixtures directory: %v", err)
	}

	for _, fixture := range fixtures {
		if !fixture.IsDir() {
			continue
		}

		fixtureName := fixture.Name()
		fixtureDir := filepath.Join(fixturesDir, fixtureName)
		testsDir := filepath.Join(fixtureDir, "tests")

		// Skip fixtures without a tests directory
		if _, err := os.Stat(testsDir); os.IsNotExist(err) {
			continue
		}

		// Get absolute path for fixture directory
		absFixtureDir, err := filepath.Abs(fixtureDir)
		if err != nil {
			t.Fatalf("failed to get absolute path for fixture %s: %v", fixtureName, err)
		}

		t.Run(fixtureName, func(t *testing.T) {
			t.Parallel()

			testscript.Run(t, testscript.Params{
				Dir:   testsDir,
				Setup: h.SetupWithFixture(absFixtureDir),
				Cmds:  h.Commands(),
			})
		})
	}
}
