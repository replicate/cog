package dockercontext

import (
	"os"
	"path/filepath"

	"github.com/replicate/cog/pkg/global"
)

// CogBuildArtifactsDirPath returns the .cog directory path, creating it
// if it doesn't exist.
func CogBuildArtifactsDirPath(dir string) (string, error) {
	cogDir := filepath.Join(dir, global.CogBuildArtifactsFolder)
	if err := os.MkdirAll(cogDir, 0o755); err != nil {
		return "", err
	}
	return cogDir, nil
}

// BuildTempDir returns the stable build staging directory at .cog/build/.
// All build-time files (requirements.txt, wheels, CA certs, schemas,
// weights manifest) are written here. The directory is created if it
// doesn't exist.
//
// The path is deterministic so that generated Dockerfile COPY instructions
// produce the same layer cache keys across builds when content is unchanged.
func BuildTempDir(dir string) (string, error) {
	buildDir := filepath.Join(dir, global.CogBuildArtifactsFolder, "build")
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		return "", err
	}
	return buildDir, nil
}
