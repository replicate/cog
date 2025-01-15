package dockerfile

import (
	"os"
	"path"
	"time"
)

func BuildCogTempDir(dir string) (string, error) {
	rootTmp := path.Join(dir, ".cog/tmp")
	if err := os.MkdirAll(rootTmp, 0o755); err != nil {
		return "", err
	}
	return rootTmp, nil
}

func BuildTempDir(dir string) (string, error) {
	rootTmp, err := BuildCogTempDir(dir)
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(rootTmp, 0o755); err != nil {
		return "", err
	}
	// tmpDir ends up being something like dir/.cog/tmp/build20240620123456.000000
	now := time.Now().Format("20060102150405.000000")
	tmpDir, err := os.MkdirTemp(rootTmp, "build"+now)
	if err != nil {
		return "", err
	}
	return tmpDir, nil
}
