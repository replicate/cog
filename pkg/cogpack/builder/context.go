package builder

import (
	"io/fs"
	"os"
	"path/filepath"

	"github.com/tonistiigi/fsutil"
)

// ContextFS represents a build context that can be used with BuildKit
type ContextFS struct {
	name      string
	fs        fsutil.FS
	tempDir   string // only set if we created a temp directory
	needsCleanup bool
}

// NewContextFromDirectory creates a ContextFS from a local directory
func NewContextFromDirectory(name, dirPath string) (*ContextFS, error) {
	fs, err := fsutil.NewFS(dirPath)
	if err != nil {
		return nil, err
	}

	return &ContextFS{
		name:         name,
		fs:           fs,
		needsCleanup: false,
	}, nil
}

// NewContextFromFS creates a ContextFS from an fs.FS by writing to a temporary directory
func NewContextFromFS(name string, filesystem fs.FS) (*ContextFS, error) {
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "cogpack-context-"+name+"-*")
	if err != nil {
		return nil, err
	}

	// Walk the filesystem and write all files to temp directory
	err = fs.WalkDir(filesystem, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		destPath := filepath.Join(tempDir, path)

		if d.IsDir() {
			return os.MkdirAll(destPath, 0755)
		}

		// Read file from fs.FS
		file, err := filesystem.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		// Create destination file
		destFile, err := os.Create(destPath)
		if err != nil {
			return err
		}
		defer destFile.Close()

		// Copy contents
		_, err = destFile.ReadFrom(file)
		return err
	})

	if err != nil {
		os.RemoveAll(tempDir)
		return nil, err
	}

	// Create fsutil.FS from temp directory
	fs, err := fsutil.NewFS(tempDir)
	if err != nil {
		os.RemoveAll(tempDir)
		return nil, err
	}

	return &ContextFS{
		name:         name,
		fs:           fs,
		tempDir:      tempDir,
		needsCleanup: true,
	}, nil
}

// Name returns the context name
func (c *ContextFS) Name() string {
	return c.name
}

// FS returns the fsutil.FS for this context
func (c *ContextFS) FS() fsutil.FS {
	return c.fs
}

// Close cleans up any temporary resources
func (c *ContextFS) Close() error {
	if c.needsCleanup && c.tempDir != "" {
		return os.RemoveAll(c.tempDir)
	}
	return nil
}