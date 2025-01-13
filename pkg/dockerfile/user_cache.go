package dockerfile

import (
	"os"
	"os/user"
	"path"
	"path/filepath"
)

const COG_CACHE_FOLDER = ".cog_cache"

func UserCache() (string, error) {
	usr, err := user.Current()
	if err != nil {
		return "", err
	}

	path := filepath.Join(usr.HomeDir, COG_CACHE_FOLDER)
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
