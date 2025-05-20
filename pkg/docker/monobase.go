package docker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/util/console"
)

const aptTarballPrefix = "apt."
const aptTarballSuffix = ".tar.zst"

func CreateAptTarball(ctx context.Context, tmpDir string, dockerClient command.Command, packages ...string) (string, error) {
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
			_, err = CreateAptTarFile(ctx, dockerClient, tmpDir, aptTarFile, packages...)
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

func CreateTarFile(ctx context.Context, dockerClient command.Command, image string, tmpDir string, tarFile string, folder string) (string, error) {
	console.Debugf("=== CreateTarFile %s %s %s %s", image, tmpDir, tarFile, folder)

	opts := command.RunOptions{
		Image: image,
		Args: []string{
			"/opt/r8/monobase/tar.sh",
			path.Join("/buildtmp", tarFile),
			"/",
			folder,
		},
		Volumes: []command.Volume{
			{
				Source:      tmpDir,
				Destination: "/buildtmp",
			},
		},
	}

	if err := dockerClient.Run(ctx, opts); err != nil {
		return "", err
	}

	return filepath.Join(tmpDir, tarFile), nil
}

func CreateAptTarFile(ctx context.Context, dockerClient command.Command, tmpDir string, aptTarFile string, packages ...string) (string, error) {
	console.Debugf("=== CreateAptTarFile %s %s", aptTarFile, packages)

	// This uses a hardcoded monobase image to produce an apt tar file.
	// The reason being that this apt tar file is created outside the docker file, and it is created by
	// running the apt.sh script on the monobase with the packages we intend to install, which produces
	// a tar file that can be untarred into a docker build to achieve the equivalent of an apt-get install.

	opts := command.RunOptions{
		Image: "r8.im/monobase:latest",
		Args: append(
			[]string{
				"/opt/r8/monobase/apt.sh",
				path.Join("/buildtmp", aptTarFile),
			},
			packages...,
		),
		Volumes: []command.Volume{
			{
				Source:      tmpDir,
				Destination: "/buildtmp",
			},
		},
	}

	if err := dockerClient.Run(ctx, opts); err != nil {
		return "", err
	}

	return aptTarFile, nil
}
