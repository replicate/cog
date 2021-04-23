package zip

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"

	"github.com/mholt/archiver/v3"
	gitignore "github.com/sabhiram/go-gitignore"

	"github.com/replicate/cog/pkg/console"
	"github.com/replicate/cog/pkg/files"
)

var defaultIgnore = []string{".cog", ".git", ".mypy_cache"}

func (z *CachingZip) WriterArchive(source string, destination io.Writer, cachedHashes []string) error {
	cachedHashSet := map[string]bool{}
	for _, hash := range cachedHashes {
		cachedHashSet[hash] = true
	}

	if err := z.zip.Create(destination); err != nil {
		return fmt.Errorf("creating zip: %v", err)
	}
	defer z.zip.Close()

	sourceInfo, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("%s: stat: %v", source, err)
	}

	ignore, err := loadIgnoreFile(source)
	if err != nil {
		return err
	}

	return filepath.Walk(source, func(fpath string, info os.FileInfo, err error) error {
		handleErr := func(err error) error {
			if z.zip.ContinueOnError {
				console.Infof("[ERROR] Walking %s: %v", fpath, err)
				return nil
			}
			return err
		}
		if err != nil {
			return handleErr(fmt.Errorf("traversing %s: %v", fpath, err))
		}
		if info == nil {
			return handleErr(fmt.Errorf("%s: no file info", fpath))
		}

		if ignore.MatchesPath(fpath) {
			return nil
		}

		// build the name to be used within the archive
		nameInArchive, err := makeNameInArchive(sourceInfo, source, "", fpath)
		if err != nil {
			return handleErr(err)
		}

		var file io.ReadCloser
		if info.Mode().IsRegular() {
			hash, err := getFileHash(fpath)
			if err != nil {
				return handleErr(err)
			}
			if cachedHashSet[hash] {
				file = io.NopCloser(bytes.NewReader([]byte(cachePrefix + hash)))
			} else {
				file, err = os.Open(fpath)
				if err != nil {
					return handleErr(fmt.Errorf("%s: opening: %v", fpath, err))
				}
				defer file.Close()
			}
		}

		err = z.zip.Write(archiver.File{
			FileInfo: archiver.FileInfo{
				FileInfo:   info,
				CustomName: nameInArchive,
			},
			ReadCloser: file,
		})
		if err != nil {
			return handleErr(fmt.Errorf("%s: writing: %s", fpath, err))
		}

		return nil
	})
}

// makeNameInArchive returns the filename for the file given by fpath to be used within
// the archive. sourceInfo is the FileInfo obtained by calling os.Stat on source, and baseDir
// is an optional base directory that becomes the root of the archive. fpath should be the
// unaltered file path of the file given to a filepath.WalkFunc.
func makeNameInArchive(sourceInfo os.FileInfo, source, baseDir, fpath string) (string, error) {
	name := filepath.Base(fpath) // start with the file or dir name
	if sourceInfo.IsDir() {
		// preserve internal directory structure; that's the path components
		// between the source directory's leaf and this file's leaf
		dir, err := filepath.Rel(filepath.Dir(source), filepath.Dir(fpath))
		if err != nil {
			return "", err
		}
		// prepend the internal directory structure to the leaf name,
		// and convert path separators to forward slashes as per spec
		name = path.Join(filepath.ToSlash(dir), name)
	}
	return path.Join(baseDir, name), nil // prepend the base directory
}

func loadIgnoreFile(dir string) (*gitignore.GitIgnore, error) {
	var ignore *gitignore.GitIgnore
	ignoreFilePath := filepath.Join(dir, ".cogignore")
	exists, err := files.Exists(ignoreFilePath)
	if err != nil {
		return nil, err
	}
	if exists {
		ignore, err = gitignore.CompileIgnoreFileAndLines(ignoreFilePath, defaultIgnore...)
		if err != nil {
			return nil, err
		}
	} else {
		ignore = gitignore.CompileIgnoreLines(defaultIgnore...)
	}
	return ignore, nil
}
