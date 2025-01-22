package docker

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

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

func FastPush(image string, projectDir string, command Command) error {
	var wg sync.WaitGroup

	token, err := command.LoadLoginToken(global.ReplicateRegistryHost)
	if err != nil {
		return fmt.Errorf("load login token error: %w", err)
	}

	tmpDir := filepath.Join(projectDir, ".cog", "tmp")
	// Setting uploadCount to 1 because we always upload the /src directory.
	uploadCount := 1
	weights, err := weights.ReadFastWeights(tmpDir)
	if err != nil {
		return fmt.Errorf("read weights error: %w", err)
	}
	uploadCount += len(weights)

	aptTarFile, err := CurrentAptTarball(tmpDir)
	if err != nil {
		return fmt.Errorf("current apt tarball error: %w", err)
	}
	if aptTarFile != "" {
		uploadCount += 1
	}

	requirementsFile, err := requirements.CurrentRequirements(tmpDir)
	if err != nil {
		return err
	}
	if requirementsFile != "" {
		uploadCount += 1
	}

	resultChan := make(chan bool, uploadCount)

	// Upload weights
	for _, weight := range weights {
		wg.Add(1)
		go uploadFile(weightsObjectType, weight.Digest, weight.Path, token, &wg, resultChan)
	}

	// Upload apt tar file
	if aptTarFile != "" {
		wg.Add(1)
		hash, err := util.SHA256HashFile(aptTarFile)
		if err != nil {
			return err
		}

		go uploadFile(filesObjectType, hash, aptTarFile, token, &wg, resultChan)
	}

	// Upload python packages.
	if requirementsFile != "" {
		wg.Add(1)
		pythonTar, err := createPythonPackagesTarFile(image, tmpDir, command)
		if err != nil {
			return err
		}

		hash, err := util.SHA256HashFile(pythonTar)
		if err != nil {
			return err
		}

		go uploadFile(filesObjectType, hash, pythonTar, token, &wg, resultChan)
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
	wg.Add(1)
	srcTar, err := createSrcTarFile(image, tmpDir, command)
	if err != nil {
		return fmt.Errorf("create src tarfile: %w", err)
	}
	hash, err := util.SHA256HashFile(srcTar)
	if err != nil {
		return err
	}
	go uploadFile(filesObjectType, hash, srcTar, token, &wg, resultChan)

	// Close the result channel after all uploads have finished
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	for result := range resultChan {
		if !result {
			return errors.New("Upload failed.")
		}
	}

	return nil
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
	url := baseURL()
	url.Path = strings.Join([]string{"", "uploads", objectType, "sha256", digest}, "/")
	url.Query().Add("size", strconv.FormatInt(size, 10))
	return url
}

func uploadFile(objectType string, digest string, path string, token string, wg *sync.WaitGroup, resultChan chan<- bool) {
	console.Debug("uploading file: " + path)
	defer wg.Done()

	info, err := os.Stat(path)
	if err != nil {
		console.Error("failed to stat file: " + path)
		resultChan <- false
		return
	}

	url := startUploadURL(objectType, digest, info.Size())
	client := &http.Client{}
	req, _ := http.NewRequest("POST", url.String(), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		console.Errorf("failed to post file: %s error: %s", path, err)
		resultChan <- false
		return
	}
	defer resp.Body.Close()

	// A conflict means we have already uploaded this file.
	if resp.StatusCode == http.StatusConflict {
		console.Debug("found file: " + path)
		resultChan <- true
		return
	} else if resp.StatusCode != http.StatusOK {
		console.Error("server error for file: " + path)
		resultChan <- false
		return
	}

	console.Debug("multi-part uploading file: " + path)
	resultChan <- false
}

func createPythonPackagesTarFile(image string, tmpDir string, command Command) (string, error) {
	return command.CreateTarFile(image, tmpDir, requirementsTarFile, "root/.venv")
}

func createSrcTarFile(image string, tmpDir string, command Command) (string, error) {
	return command.CreateTarFile(image, tmpDir, "src.tar.zst", "src")
}
