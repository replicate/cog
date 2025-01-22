package docker

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/replicate/cog/pkg/config"
)

const APT_TARBALL_PREFIX = "apt."
const APT_TARBALL_SUFFIX = ".tar.zst"

func CreateAptTarball(config *config.Config, tmpDir string, command Command) (string, error) {
	packages := config.Build.SystemPackages
	if len(packages) > 0 {
		sort.Strings(packages)
		hash := sha256.New()
		hash.Write([]byte(strings.Join(packages, " ")))
		hexHash := hex.EncodeToString(hash.Sum(nil))
		aptTarFile := APT_TARBALL_PREFIX + hexHash + APT_TARBALL_SUFFIX
		aptTarPath := filepath.Join(tmpDir, aptTarFile)

		if _, err := os.Stat(aptTarPath); errors.Is(err, os.ErrNotExist) {
			// Remove previous apt tar files.
			err = removeAptTarballs(tmpDir)
			if err != nil {
				return "", err
			}

			// Create the apt tar file
			aptTarPath, err = command.CreateAptTarFile(tmpDir, aptTarFile, packages...)
			if err != nil {
				return "", err
			}
		}

		return aptTarFile, nil
	}
	return "", nil
}

func CurrentAptTarball(tmpDir string) (string, error) {
	files, err := os.ReadDir(tmpDir)
	if err != nil {
		return "", fmt.Errorf("os read dir error: %w", err)
	}

	for _, file := range files {
		fileName := file.Name()
		if strings.HasPrefix(fileName, APT_TARBALL_PREFIX) && strings.HasSuffix(fileName, APT_TARBALL_SUFFIX) {
			return filepath.Join(tmpDir, fileName), nil
		}
	}

	return "", nil
}

func removeAptTarballs(tmpDir string) error {
	files, err := os.ReadDir(tmpDir)
	if err != nil {
		return err
	}

	for _, file := range files {
		fileName := file.Name()
		if strings.HasPrefix(fileName, APT_TARBALL_PREFIX) && strings.HasSuffix(fileName, APT_TARBALL_SUFFIX) {
			err = os.Remove(filepath.Join(tmpDir, fileName))
			if err != nil {
				return err
			}
		}
	}

	return nil
}
