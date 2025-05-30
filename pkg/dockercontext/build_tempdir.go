package dockercontext

import (
	"os"
	"path"
	"time"

	"github.com/replicate/cog/pkg/global"
)

func CogBuildArtifactsDirPath(dir string) (string, error) {
	tmpDir := path.Join(dir, global.CogBuildArtifactsFolder)
	err := os.MkdirAll(tmpDir, 0o777)
	if err != nil {
		return "", err
	}
	return tmpDir, nil
}

func CogTempDir(dir string, contextDir string) (string, error) {
	tmpDir, err := CogBuildArtifactsDirPath(dir)
	if err != nil {
		return "", err
	}
	return path.Join(tmpDir, "tmp", contextDir), nil
}

func BuildCogTempDir(dir string, subDir string) (string, error) {
	rootTmp, err := CogTempDir(dir, subDir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(rootTmp, 0o777); err != nil {
		return "", err
	}
	return rootTmp, nil
}

func BuildTempDir(dir string) (string, error) {
	// tmpDir ends up being something like dir/.cog/tmp/build20240620123456.000000
	now := time.Now().Format("20060102150405.000000")
	return BuildCogTempDir(dir, "build"+now)
}
