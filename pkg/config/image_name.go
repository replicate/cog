package config

import (
	"path"
	"regexp"
	"strings"
)

// DockerImageName returns the default Docker image name for images
func DockerImageName(projectDir string) string {
	prefix := "cog-"
	projectName := strings.ToLower(path.Base(projectDir))

	// Convert whitespace to dashes
	projectName = strings.ReplaceAll(projectName, " ", "-")

	// Remove anything non-alphanumeric
	reg := regexp.MustCompile(`[^a-z0-9\-]+`)
	projectName = reg.ReplaceAllString(projectName, "")

	// Limit to 30 characters (max Docker image name length)
	length := 30 - len(prefix)
	if len(projectName) > length {
		projectName = projectName[:length]
	}

	if !strings.HasPrefix(projectName, prefix) {
		projectName = prefix + projectName
	}

	return projectName
}

// BaseDockerImageName returns the Docker image name for base images
// used by dev-mode commands (cog serve, predict, run, train).
// These are tagged with "-base" to avoid overwriting images built by "cog build".
func BaseDockerImageName(projectDir string) string {
	return DockerImageName(projectDir) + "-base"
}
