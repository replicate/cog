package docker

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/replicate/cog/pkg/dockerfile"
	"github.com/replicate/cog/pkg/util/console"
)

const WEIGHTS_OBJECT_TYPE = "weights"
const FILES_OBJECT_TYPE = "files"
const REQUIREMENTS_TAR_FILE = "requirements.tar.zst"
const SCHEME_ENV = "R8_PUSH_SCHEME"
const HOST_ENV = "R8_PUSH_HOST"

func FastPush(image string, projectDir string, command Command) error {
	var wg sync.WaitGroup

	token, err := command.LoadLoginToken(dockerfile.BaseImageRegistry)
	if err != nil {
		return fmt.Errorf("load login token error: %w", err)
	}

	tmpDir := filepath.Join(projectDir, ".cog", "tmp")
	// Setting uploadCount to 1 because we always upload the /src directory.
	uploadCount := 1
	weights, err := dockerfile.ReadWeights(tmpDir)
	if err != nil {
		return fmt.Errorf("read weights error: %w", err)
	}
	uploadCount += len(weights)

	aptTarFile, err := dockerfile.CurrentAptTarball(tmpDir)
	if err != nil {
		return fmt.Errorf("current apt tarball error: %w", err)
	}
	if aptTarFile != "" {
		uploadCount += 1
	}

	requirementsFile, err := dockerfile.CurrentRequirements(tmpDir)
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
		go uploadFile(WEIGHTS_OBJECT_TYPE, weight.Digest, weight.Path, token, &wg, resultChan)
	}

	// Upload apt tar file
	if aptTarFile != "" {
		wg.Add(1)
		hash, err := dockerfile.SHA256HashFile(aptTarFile)
		if err != nil {
			return err
		}

		go uploadFile(FILES_OBJECT_TYPE, hash, aptTarFile, token, &wg, resultChan)
	}

	// Upload python packages.
	if requirementsFile != "" {
		wg.Add(1)
		pythonTar, err := createPythonPackagesTarFile(image, tmpDir)
		if err != nil {
			return err
		}

		hash, err := dockerfile.SHA256HashFile(pythonTar)
		if err != nil {
			return err
		}

		go uploadFile(FILES_OBJECT_TYPE, hash, pythonTar, token, &wg, resultChan)
	} else {
		requirementsTarFile := filepath.Join(tmpDir, REQUIREMENTS_TAR_FILE)
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
	srcTar, err := createSrcTarFile(image, tmpDir)
	if err != nil {
		return err
	}
	hash, err := dockerfile.SHA256HashFile(srcTar)
	if err != nil {
		return err
	}
	go uploadFile(FILES_OBJECT_TYPE, hash, srcTar, token, &wg, resultChan)

	console.Info("WAIT!!!")
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
	scheme := os.Getenv(SCHEME_ENV)
	if scheme == "" {
		scheme = "https"
	}
	host := os.Getenv(HOST_ENV)
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

func createPythonPackagesTarFile(image string, tmpDir string) (string, error) {
	return createTarFile(image, tmpDir, REQUIREMENTS_TAR_FILE, "root/.venv")
}

func createSrcTarFile(image string, tmpDir string) (string, error) {
	return createTarFile(image, tmpDir, "src.tar.zst", "src")
}

func createTarFile(image string, tmpDir string, tarFile string, folder string) (string, error) {
	args := []string{
		"run",
		"--rm",
		"--volume",
		tmpDir + ":/buildtmp",
		image,
		"/opt/r8/monobase/tar.sh",
		"/buildtmp/" + tarFile,
		"/",
		folder,
	}
	cmd := exec.Command("docker", args...)
	cmd.Stderr = os.Stderr
	console.Debug("$ " + strings.Join(cmd.Args, " "))
	err := cmd.Run()
	if err != nil {
		return "", err
	}
	return filepath.Join(tmpDir, tarFile), nil
}
