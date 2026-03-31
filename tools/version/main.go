// Command version manages the project version.
//
// Usage:
//
//	go run ./tools/version show          # print current version
//	go run ./tools/version check         # verify VERSION.txt matches Cargo.toml
//	go run ./tools/version bump 0.18.0   # bump version everywhere and commit
package main

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

const (
	versionFile = "VERSION.txt"
	cargoToml   = "crates/Cargo.toml"
)

// semverRe validates MAJOR.MINOR.PATCH with optional pre-release suffix.
// Build metadata (+build) is intentionally excluded — cog doesn't use it.
var semverRe = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9.]+)?$`)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "show":
		v, err := readVersion()
		if err != nil {
			fatal(err)
		}
		fmt.Println(v)

	case "check":
		if err := check(); err != nil {
			fatal(err)
		}

	case "bump":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: version bump <version>")
			os.Exit(1)
		}
		if err := bump(os.Args[2]); err != nil {
			fatal(err)
		}

	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: version <show|check|bump> [args]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  show    Print current version from VERSION.txt")
	fmt.Fprintln(os.Stderr, "  check   Verify VERSION.txt matches crates/Cargo.toml")
	fmt.Fprintln(os.Stderr, "  bump    Update version everywhere and commit")
}

// readVersion reads and trims VERSION.txt.
func readVersion() (string, error) {
	b, err := os.ReadFile(versionFile)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", versionFile, err)
	}
	return strings.TrimSpace(string(b)), nil
}

// cargoVersion extracts the workspace version from crates/Cargo.toml.
func cargoVersion() (string, error) {
	b, err := os.ReadFile(cargoToml)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", cargoToml, err)
	}
	v, err := parseCargoVersion(string(b))
	if err != nil {
		return "", fmt.Errorf("%s: %w", cargoToml, err)
	}
	return v, nil
}

// parseCargoVersion extracts the first `version = "..."` value from TOML content.
// This matches the workspace version in crates/Cargo.toml because it appears before
// any dependency versions, which use inline-table syntax (`{ version = "1" }`) and
// don't match the bare `version = "` prefix.
func parseCargoVersion(content string) (string, error) {
	for line := range strings.SplitSeq(content, "\n") {
		line = strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(line, "version = \""); ok {
			v = strings.TrimSuffix(v, "\"")
			return v, nil
		}
	}
	return "", fmt.Errorf("no version field found")
}

// check verifies VERSION.txt matches crates/Cargo.toml.
func check() error {
	fileVer, err := readVersion()
	if err != nil {
		return err
	}

	if !semverRe.MatchString(fileVer) {
		return fmt.Errorf("invalid semver in %s: %q", versionFile, fileVer)
	}

	cargoVer, err := cargoVersion()
	if err != nil {
		return err
	}

	if fileVer != cargoVer {
		return fmt.Errorf("version mismatch: %s has %q but %s has %q\nRun: mise run version:bump %s",
			versionFile, fileVer, cargoToml, cargoVer, fileVer)
	}

	fmt.Printf("ok: %s (%s)\n", fileVer, versionFile)
	return nil
}

// bump updates VERSION.txt and Cargo.toml, then commits.
func bump(newVersion string) error {
	if !semverRe.MatchString(newVersion) {
		return fmt.Errorf("invalid version %q — expected MAJOR.MINOR.PATCH[-prerelease]", newVersion)
	}

	// Reject if working tree is dirty.
	if dirty, err := gitDirty(); err != nil {
		return err
	} else if dirty {
		return fmt.Errorf("working tree has uncommitted changes — commit or stash first")
	}

	oldVersion, err := readVersion()
	if err != nil {
		return err
	}
	if oldVersion == newVersion {
		fmt.Printf("already at %s\n", newVersion)
		return nil
	}

	fmt.Printf("bumping: %s → %s\n", oldVersion, newVersion)

	// Write VERSION.txt.
	if err := os.WriteFile(versionFile, []byte(newVersion+"\n"), 0o644); err != nil { //nolint:gosec // version is validated semver, not user path
		return fmt.Errorf("writing %s: %w", versionFile, err)
	}
	fmt.Printf("  updated %s\n", versionFile)

	// Update Cargo.toml.
	if err := replaceCargoVersion(oldVersion, newVersion); err != nil {
		return err
	}
	fmt.Printf("  updated %s\n", cargoToml)

	// Update Cargo.lock via cargo check.
	cmd := exec.Command("cargo", "check", "--manifest-path", cargoToml, "--quiet")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "  warning: cargo check failed (%v), Cargo.lock may be stale\n", err)
	} else {
		fmt.Println("  updated Cargo.lock")
	}

	// Commit.
	gitAdd := exec.Command("git", "add", versionFile, cargoToml, "crates/Cargo.lock")
	gitAdd.Stderr = os.Stderr
	if err := gitAdd.Run(); err != nil {
		return fmt.Errorf("git add: %w", err)
	}

	msg := fmt.Sprintf("Bump version to %s", newVersion)
	gitCommit := exec.Command("git", "commit", "-m", msg) //nolint:gosec // version is validated semver, not arbitrary input
	gitCommit.Stdout = os.Stdout
	gitCommit.Stderr = os.Stderr
	if err := gitCommit.Run(); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}

	fmt.Printf("\ndone. push when ready.\n")
	return nil
}

// replaceCargoVersion replaces the version line in crates/Cargo.toml on disk.
func replaceCargoVersion(oldVersion, newVersion string) error {
	b, err := os.ReadFile(cargoToml)
	if err != nil {
		return fmt.Errorf("reading %s: %w", cargoToml, err)
	}

	updated, err := replaceVersionInCargo(string(b), oldVersion, newVersion)
	if err != nil {
		return fmt.Errorf("%s: %w", cargoToml, err)
	}

	if err := os.WriteFile(cargoToml, []byte(updated), 0o644); err != nil { //nolint:gosec // writing to known file path
		return fmt.Errorf("writing %s: %w", cargoToml, err)
	}

	// Verify round-trip.
	actual, err := cargoVersion()
	if err != nil {
		return err
	}
	if actual != newVersion {
		return fmt.Errorf("verification failed: %s has %q after update, expected %q", cargoToml, actual, newVersion)
	}

	return nil
}

// replaceVersionInCargo replaces the version line in Cargo.toml content.
func replaceVersionInCargo(content, oldVersion, newVersion string) (string, error) {
	oldLine := fmt.Sprintf("version = %q", oldVersion)
	newLine := fmt.Sprintf("version = %q", newVersion)

	if !strings.Contains(content, oldLine) {
		return "", fmt.Errorf("does not contain %s", oldLine)
	}

	return strings.Replace(content, oldLine, newLine, 1), nil
}

// gitDirty returns true if the working tree has uncommitted or untracked changes.
func gitDirty() (bool, error) {
	// Check for staged and unstaged changes to tracked files.
	cmd := exec.Command("git", "diff", "--quiet")
	if err := cmd.Run(); err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return true, nil
		}
		return false, fmt.Errorf("git diff: %w", err)
	}

	cmd = exec.Command("git", "diff", "--cached", "--quiet")
	if err := cmd.Run(); err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return true, nil
		}
		return false, fmt.Errorf("git diff --cached: %w", err)
	}

	// Check for untracked files (someone may have work-in-progress that
	// shouldn't be swept into the version bump commit).
	out, err := exec.Command("git", "ls-files", "--others", "--exclude-standard").Output()
	if err != nil {
		return false, fmt.Errorf("git ls-files: %w", err)
	}
	if len(strings.TrimSpace(string(out))) > 0 {
		return true, nil
	}

	return false, nil
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "error: %s\n", err)
	os.Exit(1)
}
