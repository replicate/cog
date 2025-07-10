package project

import (
	"io/fs"
	"os"

	"github.com/replicate/cog/pkg/config"
)

// SourceInfo contains high-level information extracted from the project that
// Providers may use when deciding what Steps to emit. It’s intentionally
// minimal for now.
type SourceInfo struct {
	Config *config.Config
	FS     *SourceFS
}

func (s *SourceInfo) Close() error {
	return s.FS.Close()
}

func NewSourceInfo(rootPath string, config *config.Config) (*SourceInfo, error) {
	fs, err := NewSourceFS(rootPath)
	if err != nil {
		return nil, err
	}

	return &SourceInfo{
		Config: config,
		FS:     fs,
	}, nil
}

type SourceFS struct {
	fs.FS

	root *os.Root
	path string
}

func NewSourceFS(path string) (*SourceFS, error) {
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}

	return &SourceFS{
		FS:   root.FS(),
		root: root,
		path: path,
	}, nil
}

func (s *SourceFS) Close() error {
	return s.root.Close()
}

func (s *SourceFS) GlobExists(pattern string) bool {
	files, err := fs.Glob(s.FS, pattern)
	if err != nil {
		return false
	}
	return len(files) > 0
}
