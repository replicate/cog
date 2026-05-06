package files

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/mitchellh/go-homedir"
	"github.com/vincent-petithory/dataurl"

	r8_path "github.com/replicate/cog/pkg/path"
	"github.com/replicate/cog/pkg/util/mime"
)

var (
	ErrorFailedToSplitDataURL = errors.New("Failed to split data URL into 2 parts")
)

func Exists(path string) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return true, nil
	} else if errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else {
		return false, fmt.Errorf("Failed to determine if %s exists: %w", path, err)
	}
}

func IsEmpty(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return true, nil
		}
		return false, err
	}
	return len(entries) == 0, nil
}

func WriteDataURLToFile(url string, destination string) (string, error) {
	if strings.HasPrefix(url, "data:None;base64") {
		url = strings.Replace(url, "data:None;base64", "data:;base64", 1)
	}
	dataurlObj, err := dataurl.DecodeString(url)
	if err != nil {
		// Attempt to fallback to binary base64 file decode.
		parts := strings.SplitN(url, ",", 2)
		if len(parts) != 2 {
			return "", ErrorFailedToSplitDataURL
		}
		base64Data := parts[1]
		url = "data:;base64," + base64Data
		dataurlObj, err = dataurl.DecodeString(url)
		if err != nil {
			return "", fmt.Errorf("Failed to decode data URL: %w", err)
		}
	}
	output := dataurlObj.Data

	ext := path.Ext(destination)
	dir := path.Dir(destination)
	name := r8_path.TrimExt(path.Base(destination))

	// Check if ext is an integer, in which case ignore it...
	if r8_path.IsExtInteger(ext) {
		ext = ""
		name = path.Base(destination)
	}

	if ext == "" {
		ext = mime.ExtensionByType(dataurlObj.ContentType())
	}

	path, err := WriteFile(output, path.Join(dir, name+ext))
	if err != nil {
		return "", err
	}

	return path, nil
}

// AtomicWrite writes data to path via a temp file + fsync + rename so
// readers never see a partial file. Parent directories are created as
// needed. Modeled after tailscale.com/atomicfile.
func AtomicWrite(path string, data []byte) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create directory for %s: %w", path, err)
	}
	f, err := os.CreateTemp(dir, filepath.Base(path)+".tmp")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", path, err)
	}
	tmpPath := f.Name()
	defer func() {
		if err != nil {
			_ = f.Close()
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	return os.Rename(tmpPath, filepath.Clean(path)) //nolint:gosec // path is constructed internally
}

// Copy copies src to dst, creating parent directories as needed.
func Copy(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read %s: %w", src, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("create directory for %s: %w", dst, err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", dst, err)
	}
	return nil
}

func WriteFile(output []byte, outputPath string) (string, error) {
	outputPath, err := homedir.Expand(outputPath)
	if err != nil {
		return "", err
	}

	// Write to file
	outFile, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return "", err
	}

	if _, err := outFile.Write(output); err != nil {
		return "", err
	}
	if err := outFile.Close(); err != nil {
		return "", err
	}
	return outputPath, nil
}
