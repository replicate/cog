package runner

import (
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
)

func copyRecursive(srcRoot, dstRoot string) error {
	return filepath.WalkDir(srcRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(srcRoot, path)
		if err != nil {
			return err
		}

		dstPath := filepath.Join(dstRoot, relPath)

		info, err := d.Info()
		if err != nil {
			return err
		}

		if d.IsDir() {
			if path != srcRoot {
				return os.MkdirAll(dstPath, info.Mode())
			}
			return nil
		}
		bs, err := os.ReadFile(path) //nolint:gosec // expected dynamic path
		if err != nil {
			return err
		}
		return os.WriteFile(dstPath, bs, info.Mode())
	})
}

func PrepareProcedureSourceURL(srcURL, runnerID string) (string, error) {
	sha := sha256.New()
	sha.Write([]byte(srcURL))
	dstDir := filepath.Join(os.TempDir(), fmt.Sprintf("procedure-%x-%s", sha.Sum(nil), runnerID))

	if err := os.RemoveAll(dstDir); err != nil {
		return "", err
	}

	u, err := url.Parse(srcURL)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "file":
		stat, err := os.Stat(u.Path) // #nosec G304 -- TODO[md]: add path validation to ensure within allowed directories
		if err != nil {
			return "", err
		}
		if !stat.IsDir() {
			return "", fmt.Errorf("invalid procedure source URL: %s", srcURL)
		}
		err = os.MkdirAll(dstDir, 0o700)
		if err != nil {
			return "", err
		}
		err = copyRecursive(u.Path, dstDir)
		if err != nil {
			return "", err
		}
		return dstDir, nil
	case "http", "https":
		resp, err := http.Get(srcURL) //nolint:gosec // TODO[md]: add SSRF protection
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		tarball, err := os.CreateTemp("", "procedure-tarball-*")
		if err != nil {
			return "", err
		}
		if _, err := io.Copy(tarball, resp.Body); err != nil {
			return "", err
		}
		defer func() {
			_ = os.Remove(tarball.Name())
		}()

		if err := os.MkdirAll(dstDir, 0o700); err != nil {
			return "", err
		}
		cmd := exec.Command("tar", "-xf", tarball.Name(), "-C", dstDir) //nolint:gosec // expected subprocess launched with variable
		if err := cmd.Run(); err != nil {
			return "", err
		}
		return dstDir, nil
	}
	return "", fmt.Errorf("invalid procedure source URL: %s", srcURL)
}
