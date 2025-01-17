package docker

import (
	"errors"
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

func FastPush(image string, projectDir string) error {
	var wg sync.WaitGroup

	tmpDir := filepath.Join(projectDir, ".cog", "tmp")
	// Setting uploadCount to 1 because we always upload the /src directory.
	uploadCount := 1
	weights, err := dockerfile.ReadWeights(tmpDir)
	if err != nil {
		return err
	}
	uploadCount += len(weights)

	aptTarFile, err := dockerfile.CurrentAptTarball(tmpDir)
	if err != nil {
		return err
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
		go uploadFile(WEIGHTS_OBJECT_TYPE, weight.Digest, weight.Path, &wg, resultChan)
	}

	// Upload apt tar file
	if aptTarFile != "" {
		wg.Add(1)
		hash, err := dockerfile.SHA256HashFile(aptTarFile)
		if err != nil {
			return err
		}

		go uploadFile(FILES_OBJECT_TYPE, hash, aptTarFile, &wg, resultChan)
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

		go uploadFile(FILES_OBJECT_TYPE, hash, pythonTar, &wg, resultChan)
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
	go uploadFile(FILES_OBJECT_TYPE, hash, srcTar, &wg, resultChan)

	wg.Wait()

	for result := range resultChan {
		if !result {
			return errors.New("Upload failed.")
		}
	}

	return nil
}

func baseURL() url.URL {
	return url.URL{
		Scheme: "https",
		Host:   "monobeam.replicate.delivery",
	}
}

func startUploadURL(objectType string, digest string, size int64) url.URL {
	url := baseURL()
	url.Path = strings.Join([]string{"", "uploads", objectType, "sha256", digest}, "/")
	url.Query().Add("size", strconv.FormatInt(size, 10))
	return url
}

func uploadFile(objectType string, digest string, path string, wg *sync.WaitGroup, resultChan chan<- bool) {
	console.Debug("uploading file: " + path)
	defer wg.Done()

	info, err := os.Stat(path)
	if err != nil {
		console.Debug("failed to stat file: " + path)
		resultChan <- false
		return
	}

	url := startUploadURL(objectType, digest, info.Size())
	resp, err := http.Post(url.String(), "", nil)
	if err != nil {
		console.Debug("failed to post file: " + path)
		resultChan <- false
		return
	}

	// A conflict means we have already uploaded this file.
	if resp.StatusCode == http.StatusConflict {
		console.Debug("found file: " + path)
		resultChan <- true
		return
	} else if resp.StatusCode != http.StatusOK {
		console.Debug("server error for file: " + path)
		resultChan <- false
		return
	}

	console.Debug("multi-part uploading file: " + path)
}

func createPythonPackagesTarFile(image string, tmpDir string) (string, error) {
	const requirementsTarFile = "requirements.tar.zst"
	return createTarFile(image, tmpDir, requirementsTarFile, "root/.venv")
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
