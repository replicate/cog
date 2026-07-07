package requirements

import (
	"fmt"
	"path/filepath"
	"strings"
)

var localArtifactSuffixes = []string{
	".whl",
	".zip",
	".tar.gz",
	".tgz",
	".tar.bz2",
	".tar.xz",
}

// ParseLocalArtifactRequirement identifies simple local wheel/source-archive
// requirement lines. It intentionally does not parse full pip requirement
// syntax; callers should reject unsupported local forms with clear errors.
func ParseLocalArtifactRequirement(line string) (string, bool, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", false, nil
	}

	if strings.HasPrefix(line, "file:") || strings.Contains(line, " @ file:") {
		return "", false, fmt.Errorf("local file URL requirements are not supported: %s", line)
	}
	if option, ok := parseUnsupportedLocalOption(line); ok {
		return "", false, fmt.Errorf("local requirements option %q is not supported: %s", option, line)
	}
	if strings.HasPrefix(line, "-") || isRemoteRequirement(line) {
		return "", false, nil
	}

	fields := strings.Fields(line)
	if len(fields) > 1 {
		if hasLocalArtifactSuffix(fields[0]) || isLocalPath(fields[0]) {
			return "", false, fmt.Errorf("local package artifact requirements do not support inline options or hashes: %s", line)
		}
		return "", false, nil
	}

	if !isLocalPath(line) && !hasLocalArtifactSuffix(line) {
		return "", false, nil
	}
	if !hasLocalArtifactSuffix(line) {
		return "", false, fmt.Errorf("local package requirement %q is not a supported wheel or source archive", line)
	}

	return line, true, nil
}

func parseUnsupportedLocalOption(line string) (string, bool) {
	for _, option := range []string{"--find-links", "--requirement"} {
		if value, ok := optionValue(line, option); ok && isLocalOptionValue(value) {
			return option, true
		}
	}
	for _, option := range []string{"-f", "-r"} {
		if value, ok := shortOptionValue(line, option); ok && isLocalOptionValue(value) {
			return option, true
		}
	}
	return "", false
}

func optionValue(line string, option string) (string, bool) {
	if value, ok := strings.CutPrefix(line, option+"="); ok {
		return strings.TrimSpace(value), true
	}
	if value, ok := strings.CutPrefix(line, option+" "); ok {
		return strings.TrimSpace(value), true
	}
	return "", false
}

func shortOptionValue(line string, option string) (string, bool) {
	if value, ok := strings.CutPrefix(line, option+" "); ok {
		return strings.TrimSpace(value), true
	}
	return "", false
}

func isLocalOptionValue(value string) bool {
	if value == "" || isRemoteRequirement(value) || strings.HasPrefix(value, "file:") {
		return false
	}
	return true
}

func isRemoteRequirement(line string) bool {
	return strings.Contains(line, "://") || strings.HasPrefix(line, "git+")
}

func isLocalPath(line string) bool {
	return filepath.IsAbs(line) || strings.HasPrefix(line, "./") || strings.HasPrefix(line, "../")
}

func hasLocalArtifactSuffix(path string) bool {
	path = strings.ToLower(path)
	for _, suffix := range localArtifactSuffixes {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}
	return false
}
