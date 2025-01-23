package docker

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/requirements"
	"github.com/replicate/cog/pkg/util"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/pkg/weights"
)

const weightsObjectType = "weights"
const filesObjectType = "files"
const requirementsTarFile = "requirements.tar.zst"
const schemeEnv = "R8_PUSH_SCHEME"
const hostEnv = "R8_PUSH_HOST"

func FastPush(image string, projectDir string, command Command, ctx context.Context) error {
	g, _ := errgroup.WithContext(ctx)

	token, err := command.LoadLoginToken(global.ReplicateRegistryHost)
	if err != nil {
		return fmt.Errorf("load login token error: %w", err)
	}

	tmpDir := filepath.Join(projectDir, ".cog", "tmp")
	weights, err := weights.ReadFastWeights(tmpDir)
	if err != nil {
		return fmt.Errorf("read weights error: %w", err)
	}
	// Upload weights
	for _, weight := range weights {
		g.Go(func() error {
			return uploadFile(weightsObjectType, weight.Digest, weight.Path, token)
		})
	}

	aptTarFile, err := CurrentAptTarball(tmpDir)
	if err != nil {
		return fmt.Errorf("current apt tarball error: %w", err)
	}
	// Upload apt tar file
	if aptTarFile != "" {
		hash, err := util.SHA256HashFile(aptTarFile)
		if err != nil {
			return err
		}
		g.Go(func() error {
			return uploadFile(filesObjectType, hash, aptTarFile, token)
		})
	}

	requirementsFile, err := requirements.CurrentRequirements(tmpDir)
	if err != nil {
		return err
	}
	// Upload python packages.
	if requirementsFile != "" {
		pythonTar, err := createPythonPackagesTarFile(image, tmpDir, command)
		if err != nil {
			return err
		}

		hash, err := util.SHA256HashFile(pythonTar)
		if err != nil {
			return err
		}
		g.Go(func() error {
			return uploadFile(filesObjectType, hash, pythonTar, token)
		})
	} else {
		requirementsTarFile := filepath.Join(tmpDir, requirementsTarFile)
		_, err = os.Stat(requirementsTarFile)
		if !errors.Is(err, os.ErrNotExist) {
			err = os.Remove(requirementsTarFile)
			if err != nil {
				return err
			}
		}
	}

	// Upload user /src.
	srcTar, err := createSrcTarFile(image, tmpDir, command)
	if err != nil {
		return fmt.Errorf("create src tarfile: %w", err)
	}
	hash, err := util.SHA256HashFile(srcTar)
	if err != nil {
		return err
	}
	g.Go(func() error {
		return uploadFile(filesObjectType, hash, srcTar, token)
	})

	// Wait for uploads
	return g.Wait()
}

func baseURL() url.URL {
	scheme := os.Getenv(schemeEnv)
	if scheme == "" {
		scheme = "https"
	}
	host := os.Getenv(hostEnv)
	if host == "" {
		host = "monobeam.replicate.delivery"
	}
	return url.URL{
		Scheme: scheme,
		Host:   host,
	}
}

func startUploadURL(objectType string, digest string, size int64) url.URL {
	uploadUrl := baseURL()
	uploadUrl.Path = strings.Join([]string{"", "uploads", objectType, "sha256", digest}, "/")
	uploadUrl.Query().Add("size", strconv.FormatInt(size, 10))
	return uploadUrl
}

func uploadFile(objectType string, digest string, path string, token string) error {
	console.Debug("uploading file: " + path)
	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	uploadUrl := startUploadURL(objectType, digest, info.Size())
	client := &http.Client{}
	req, _ := http.NewRequest("POST", uploadUrl.String(), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// A conflict means we have already uploaded this file.
	if resp.StatusCode == http.StatusConflict {
		return nil
	} else if resp.StatusCode != http.StatusOK {
		return errors.New("Bad response: " + strconv.Itoa(resp.StatusCode))
	}

	console.Debug("multi-part uploading file: " + path)
	return nil
}

func createPythonPackagesTarFile(image string, tmpDir string, command Command) (string, error) {
	return command.CreateTarFile(image, tmpDir, requirementsTarFile, "root/.venv")
}

func createSrcTarFile(image string, tmpDir string, command Command) (string, error) {
	return command.CreateTarFile(image, tmpDir, "src.tar.zst", "src")
}
