package requirements

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ParseLocalArtifactRequirement returns a bare local wheel or source archive
// path. Other pip requirements pass through unchanged; unsupported local forms
// return an error before the Docker build starts.
func ParseLocalArtifactRequirement(line string) (string, bool, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", false, nil
	}
	if isFileURL(line) {
		return "", false, fmt.Errorf("local file URL requirements are not supported: %s", line)
	}
	if name, target, ok := strings.Cut(line, "@"); ok {
		spaced := name != strings.TrimSpace(name) || target != strings.TrimSpace(target)
		name, target = strings.TrimSpace(name), strings.TrimSpace(target)
		if name != "" && PackageName(name) == name && isFileURL(target) {
			return "", false, fmt.Errorf("local file URL requirements are not supported: %s", line)
		}
		if name != "" && PackageName(name) == name && (isLocalPath(target) || spaced && isLocalArtifact(target)) {
			return "", false, fmt.Errorf("local direct reference requirements (`name @ path`) are not supported; list the path directly instead: %s", line)
		}
	}
	if strings.HasPrefix(line, "-") {
		if option := unsupportedLocalOption(line); option != "" {
			return "", false, fmt.Errorf("local requirements option %q is not supported: %s", option, line)
		}
		return "", false, nil
	}
	if hasInlineOption(line) {
		return "", false, fmt.Errorf("local package artifact requirements do not support inline options or hashes: %s", line)
	}
	if isRemoteRequirement(line) {
		return "", false, nil
	}
	if base, _, ok := strings.Cut(line, ";"); ok && isLocalArtifact(strings.TrimSpace(base)) {
		return "", false, fmt.Errorf("environment markers are not supported on local package artifact requirements: %s", line)
	}
	if base, _, ok := strings.Cut(line, "["); ok && strings.HasSuffix(line, "]") && isLocalArtifact(strings.TrimSpace(base)) {
		return "", false, fmt.Errorf("extras are not supported on local package artifact requirements: %s", line)
	}

	if !isLocalPath(line) && !hasLocalArtifactSuffix(line) {
		return "", false, nil
	}
	if !hasLocalArtifactSuffix(line) {
		return "", false, fmt.Errorf("local package requirement %q is not a supported wheel or source archive", line)
	}
	return line, true, nil
}

func unsupportedLocalOption(line string) string {
	for _, option := range []string{"--find-links", "--requirement", "-f", "-r"} {
		value, ok := requirementOptionValue(line, option)
		if ok && value != "" && (isFileURL(value) || !isRemoteRequirement(value)) {
			return option
		}
	}
	return ""
}

func requirementOptionValue(line string, option string) (string, bool) {
	rest, ok := strings.CutPrefix(line, option)
	if !ok || rest == "" {
		return "", false
	}
	if strings.HasPrefix(option, "--") {
		switch rest[0] {
		case '=':
			rest = rest[1:]
		case ' ', '\t':
		default:
			return "", false
		}
	}
	return strings.TrimSpace(rest), true
}

func hasInlineOption(line string) bool {
	for i := 0; i < len(line); i++ {
		if line[i] != ' ' && line[i] != '\t' {
			continue
		}
		rest := strings.TrimLeft(line[i:], " \t")
		if !isLocalArtifact(strings.TrimSpace(line[:i])) {
			continue
		}
		if strings.HasPrefix(rest, "--") {
			return true
		}
		for _, option := range []string{"-f", "-r"} {
			if _, ok := requirementOptionValue(rest, option); ok {
				return true
			}
		}
	}
	return false
}

func isLocalArtifact(path string) bool {
	return !isRemoteRequirement(path) && (isLocalPath(path) || hasLocalArtifactSuffix(path))
}

func isRemoteRequirement(line string) bool {
	return strings.Contains(line, "://") || strings.HasPrefix(line, "git+")
}

func isFileURL(line string) bool {
	return strings.HasPrefix(strings.ToLower(line), "file:")
}

func isLocalPath(line string) bool {
	return filepath.IsAbs(line) || strings.HasPrefix(line, "./") || strings.HasPrefix(line, "../")
}

func hasLocalArtifactSuffix(path string) bool {
	path = strings.ToLower(path)
	return strings.HasSuffix(path, ".whl") ||
		strings.HasSuffix(path, ".zip") ||
		strings.HasSuffix(path, ".tar.gz") ||
		strings.HasSuffix(path, ".tgz") ||
		strings.HasSuffix(path, ".tar.bz2") ||
		strings.HasSuffix(path, ".tar.xz")
}
