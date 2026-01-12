// Package harness provides utilities for running cog integration tests.
package harness

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/rogpeppe/go-internal/testscript"
)

// Harness provides utilities for running cog integration tests.
type Harness struct {
	CogBinary string
}

// New creates a new Harness, resolving the cog binary location.
func New() (*Harness, error) {
	cogBinary, err := ResolveCogBinary()
	if err != nil {
		return nil, err
	}
	return &Harness{CogBinary: cogBinary}, nil
}

// ResolveCogBinary finds the cog binary to use for tests.
// It checks (in order):
// 1. COG_BINARY environment variable
// 2. "cog" in PATH
func ResolveCogBinary() (string, error) {
	if cogBinary := os.Getenv("COG_BINARY"); cogBinary != "" {
		if !filepath.IsAbs(cogBinary) {
			cwd, err := os.Getwd()
			if err != nil {
				return "", err
			}
			cogBinary = filepath.Join(cwd, cogBinary)
		}
		return cogBinary, nil
	}

	// Fall back to cog in PATH
	cogPath, err := exec.LookPath("cog")
	if err != nil {
		return "", err
	}
	return cogPath, nil
}

// Commands returns the custom testscript commands provided by this harness.
func (h *Harness) Commands() map[string]func(ts *testscript.TestScript, neg bool, args []string) {
	return map[string]func(ts *testscript.TestScript, neg bool, args []string){
		"cog": h.cmdCog,
	}
}

// cmdCog implements the 'cog' command for testscript.
func (h *Harness) cmdCog(ts *testscript.TestScript, neg bool, args []string) {
	err := ts.Exec(h.CogBinary, args...)
	if neg {
		if err == nil {
			ts.Fatalf("cog command succeeded unexpectedly")
		}
	} else {
		if err != nil {
			ts.Fatalf("cog command failed: %v", err)
		}
	}
}

// SetupWithFixture returns a testscript Setup function that copies fixture files
// into the work directory and configures the test environment.
func (h *Harness) SetupWithFixture(fixtureDir string) func(*testscript.Env) error {
	return func(env *testscript.Env) error {
		// Copy fixture files into the work directory
		if err := copyDir(fixtureDir, env.WorkDir); err != nil {
			return err
		}

		// Disable update checks during tests
		env.Setenv("COG_NO_UPDATE_CHECK", "1")
		return nil
	}
}

// copyDir recursively copies a directory tree, excluding the "tests" subdirectory.
func copyDir(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		// Skip the tests directory - we don't want to copy test files into the work dir
		if entry.Name() == "tests" {
			continue
		}

		if entry.IsDir() {
			if err := os.MkdirAll(dstPath, 0755); err != nil {
				return err
			}
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}
	return nil
}

// copyFile copies a single file from src to dst.
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}

	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}
