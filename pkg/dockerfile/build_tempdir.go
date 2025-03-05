package dockerfile

import (
	"os"
	"path"
	"time"
)

func BuildCogTempDir(dir string, subDir string) (string, error) {
	rootTmp := path.Join(dir, ".cog", "tmp", subDir)
	if err := os.MkdirAll(rootTmp, 0o755); err != nil {
		return "", err
	}
	return rootTmp, nil
}

func BuildTempDir(dir string) (string, error) {
	// tmpDir ends up being something like dir/.cog/tmp/build20240620123456.000000
	now := time.Now().Format("20060102150405.000000")
	return BuildCogTempDir(dir, "build"+now)
}
