package config

import (
	"log"
	"path"
	"regexp"
	"strings"
)

// DockerImageName returns the default Docker image name for images
func DockerImageName(projectDir string) string {
	prefix := "cog-"
	projectName := strings.ToLower(path.Base(projectDir))

	// Convert whitespace to dashes
	projectName = strings.Replace(projectName, " ", "-", -1)

	// Remove anything non-alphanumeric
	reg, err := regexp.Compile(`[^a-z0-9\-]+`)
	if err != nil {
		log.Fatal(err)
	}
	projectName = reg.ReplaceAllString(projectName, "")

	// Limit to 30 characters (max Docker image name length)
	length := 30 - len(prefix)
	if len(projectName) > length {
		projectName = projectName[:length]
	}

	return prefix + projectName
}

// BaseDockerImageName returns the Docker image name for base images
func BaseDockerImageName(projectDir string) string {
	return DockerImageName(projectDir) + "-base"
}
