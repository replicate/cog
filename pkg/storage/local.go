package storage

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	log "github.com/sirupsen/logrus"

	"github.com/replicate/cog/pkg/files"
)

type LocalStorage struct {
	rootDir string
}

func NewLocalStorage(rootDir string) (*LocalStorage, error) {
	exists, err := files.FileExists(rootDir)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("Root directory %s doesn't exist", rootDir)
	}
	isDir, err := files.IsDir(rootDir)
	if err != nil {
		return nil, err
	}
	if !isDir {
		return nil, fmt.Errorf("Root path %s is not a directory", rootDir)
	}
	db := &LocalStorage{
		rootDir: rootDir,
	}
	return db, nil
}

func (s *LocalStorage) Upload(reader io.Reader, id string) error {
	path := s.pathForID(id)
	log.Debugf("Saving to %s", path)
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("Failed to create %s: %w", path, err)
	}
	if _, err := io.Copy(file, reader); err != nil {
		return fmt.Errorf("Failed to write %s: %w", path, err)
	}
	return nil
}

func (s *LocalStorage) Download(id string) ([]byte, error) {
	path := s.pathForID(id)
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("Failed to read %s: %w", path, err)
	}
	return contents, nil
}

func (s *LocalStorage) pathForID(id string) string {
	return filepath.Join(s.rootDir, id + ".zip")
}
