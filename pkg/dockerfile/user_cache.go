package dockerfile

import (
	"os"
	"path"
	"path/filepath"
)

func UserCache() (string, error) {
	path, err := filepath.Abs("~/.cog/cache")
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return "", err
	}
	return path, nil
}

func UserCacheFolder(folder string) (string, error) {
	userCache, err := UserCache()
	if err != nil {
		return "", err
	}
	cacheFolder := path.Join(userCache, folder)
	if err := os.MkdirAll(cacheFolder, 0o755); err != nil {
		return "", err
	}
	return cacheFolder, nil
}
