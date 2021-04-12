package zip

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/replicate/cog/pkg/files"
)

// TODO(andreas): garbage collection

type CacheFileSystem struct {
	dir string
}

func NewRepoCache(user string, repoName string) (*CacheFileSystem, error) {
	return NewCacheFileSystem(fmt.Sprintf(".cog/zip-cache/%s/%s", user, repoName))
}

func NewCacheFileSystem(cacheDir string) (*CacheFileSystem, error) {
	exists, err := files.Exists(cacheDir)
	if err != nil {
		return nil, err
	}
	if !exists {
		if err := os.MkdirAll(cacheDir, 0755); err != nil {
			return nil, err
		}
	}
	return &CacheFileSystem{dir: cacheDir}, nil
}

func (c *CacheFileSystem) GetHashes() ([]string, error) {
	f, err := os.Open(c.dir)
	if err != nil {
		return nil, fmt.Errorf("Failed to open %s: %v", c.dir, err)
	}
	names, err := f.Readdirnames(0)
	if err != nil {
		return nil, fmt.Errorf("Failed to list directory %s: %v", c.dir, err)
	}
	return names, nil
}

func (c *CacheFileSystem) load(hash string, path string) error {
	return files.CopyFile(filepath.Join(c.dir, hash), path)
}

func (c *CacheFileSystem) save(hash string, path string) error {
	return files.CopyFile(path, filepath.Join(c.dir, hash))
}
