package docker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/replicate/cog/pkg/docker/command"
)

const aptTarballPrefix = "apt."
const aptTarballSuffix = ".tar.zst"

func CreateAptTarball(ctx context.Context, tmpDir string, dockerCommand command.Command, packages ...string) (string, error) {
	if len(packages) > 0 {
		sort.Strings(packages)
		hash := sha256.New()
		hash.Write([]byte(strings.Join(packages, " ")))
		hexHash := hex.EncodeToString(hash.Sum(nil))
		aptTarFile := aptTarballPrefix + hexHash + aptTarballSuffix
		aptTarPath := filepath.Join(tmpDir, aptTarFile)

		if _, err := os.Stat(aptTarPath); errors.Is(err, os.ErrNotExist) {
			// Remove previous apt tar files.
			err = removeAptTarballs(tmpDir)
			if err != nil {
				return "", err
			}

			// Create the apt tar file
			_, err = dockerCommand.CreateAptTarFile(ctx, tmpDir, aptTarFile, packages...)
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
		if strings.HasPrefix(fileName, aptTarballPrefix) && strings.HasSuffix(fileName, aptTarballSuffix) {
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
		if strings.HasPrefix(fileName, aptTarballPrefix) && strings.HasSuffix(fileName, aptTarballSuffix) {
			err = os.Remove(filepath.Join(tmpDir, fileName))
			if err != nil {
				return err
			}
		}
	}

	return nil
}
