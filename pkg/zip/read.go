package zip

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

func (z *CachingZip) ReaderUnarchive(source io.Reader, size int64, destination string, cacheFS *CacheFileSystem) error {
	if err := z.zip.ReaderUnarchive(source, size, destination); err != nil {
		return err
	}

	return filepath.Walk(destination, func(fpath string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}

		prefixBuf := make([]byte, len(cachePrefix))
		file, err := os.Open(fpath)
		if err != nil {
			return fmt.Errorf("Failed to open in zip %s: %v", fpath, err)
		}
		defer file.Close()
		if _, err := file.Read(prefixBuf); err != nil {
			if err != io.EOF {
				return fmt.Errorf("Failed to read in zip %s: %v", fpath, err)
			}
		}
		if string(prefixBuf) == cachePrefix {
			hashBuf := make([]byte, hashLength)
			if _, err := file.Read(hashBuf); err != nil {
				return fmt.Errorf("Failed to read hash from %s: %v", fpath, err)
			}
			hash := string(hashBuf)
			if err := cacheFS.load(hash, fpath); err != nil {
				return err
			}
		} else {
			file.Close()
			hash, err := getFileHash(fpath)
			if err != nil {
				return err
			}
			if err := cacheFS.save(hash, fpath); err != nil {
				return err
			}
		}

		return nil
	})
}
