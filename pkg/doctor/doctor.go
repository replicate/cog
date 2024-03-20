package doctor

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	"github.com/bmatcuk/doublestar/v4"
	"github.com/vbauerster/mpb/v7"
	"github.com/vbauerster/mpb/v7/decor"
	"gopkg.in/yaml.v3"

	w "github.com/replicate/cog/pkg/weights"
)

var problematicPrefixes = []string{".cog", ".git", "__pycache__"}

var suffixesToIgnore = []string{
	".py", ".ipynb", ".whl", // Python projects
	".jpg", ".jpeg", ".png", ".webp", ".svg", ".gif", ".avif", ".heic", // images
	".mp4", ".mov", ".avi", ".wmv", ".mkv", ".webm", // videos
	".mp3", ".wav", ".ogg", ".flac", ".aac", ".m4a", // audio files
	".log", // logs
}

type FileWalker func(root string, walkFn filepath.WalkFunc) error

func CheckFiles() error {

	ignore, err := parseDockerignore()
	if err != nil {
		return err
	}

	problemDirs, weightFiles, err := walk(filepath.Walk, ignore)
	if err != nil {
		return err
	}

	if len(problemDirs) > 0 {
		fmt.Println("These directories can likely be excluded from your image:")
		for _, dir := range problemDirs {
			fmt.Printf("\t\033[31m%s\033[0m\n", dir)
		}
		fmt.Print("\nAutomatically add these to .dockerignore? [Y/n] ")
		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.TrimSpace(response)
		if strings.EqualFold(response, "y") || response == "" {
			err := addDockerignoreEntries(problemDirs)
			if err != nil {
				return err
			}
			fmt.Println("Added entries to .dockerignore successfully!")
		}
		fmt.Print("\n\n")
	}

	if len(weightFiles) > 0 {
		fmt.Println("These files are large and better excluded from your image:")
		for _, file := range weightFiles {
			fmt.Printf("\t\033[32m%s\033[0m\n", file)
		}
		fmt.Printf("\nIf you have a cache key with Replicate, please enter it now. [skip] ")
		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.TrimSpace(response)
		if response != "" {
			urls := []string{}
			for _, file := range weightFiles {
				url, err := uploadWeights(file, "replicate-weights-wqzt", response)
				if err != nil {
					return err
				}
				urls = append(urls, url)
			}
			err := writeManifest(urls, weightFiles)
			if err != nil {
				return err
			}
			err = addDockerignoreEntries(weightFiles)
			if err != nil {
				return err
			}
			err = writePgetPy()
			if err != nil {
				return err
			}
			err = installPGet()
			if err != nil {
				return err
			}

			fmt.Println("Added all files to cache.")
			fmt.Print("In your predictor, you can use the following code to download the weights:\n\n")
			fmt.Println("\tfrom pget import pget_manifest")
			fmt.Println("\t...")
			fmt.Print("\tclass Predictor(BasePredictor):\n\n")
			fmt.Println("\t  def setup(self):")
			fmt.Println("\t    pget_manifest()")
			fmt.Println("\t    ...")
			fmt.Println("\t}")
		}
	}

	return nil
}

const sizeThreshold = 20 * 1024 * 1024 // 20MB

func walk(fw FileWalker, ignore []string) (weights []string, problemDirs []string, e error) {
	err := fw(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		for _, pattern := range ignore {

			if strings.HasPrefix(path, strings.TrimSuffix(pattern, "/")) {
				return nil
			}
			match, err := doublestar.PathMatch(pattern, path)
			if err != nil {
				return err
			}
			if match {
				return nil
			}
		}

		// If it's a directory, we just check if it's "problematic"
		if info.IsDir() {
			for _, prefix := range problematicPrefixes {
				if strings.HasPrefix(info.Name(), prefix) {
					problemDirs = append(problemDirs, path)
				}
			}
			return nil
		}

		// Filter out files in "problematic" directories
		for _, prefix := range problematicPrefixes {
			if strings.HasPrefix(path, prefix) {
				return nil
			}
		}

		// Filter out "known" suffixes
		for _, suffix := range suffixesToIgnore {
			if strings.HasSuffix(path, suffix) {
				return nil
			}
		}

		// Filter out weights that are too small / not worth pget'ing
		if info.Size() < sizeThreshold {
			return nil
		}

		weights = append(weights, path)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	// by sorting the files by levels, we can filter out directories that are prefixes of other directories
	// e.g. /a/b/ is a prefix of /a/b/c/, so we can filter out /a/b/c/
	w.SortFilesByLevels(weights)

	return problemDirs, weights, nil
}

func parseDockerignore() ([]string, error) {

	file, err := os.Open(".dockerignore")
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) > 0 && line[0] != '#' {
			lines = append(lines, line)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return lines, nil
}

func addDockerignoreEntries(entries []string) error {
	file, err := os.OpenFile(".dockerignore", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if _, err := file.WriteString(entry + "\n"); err != nil {
			return err
		}
	}

	defer file.Close()
	return nil
}

func uploadWeights(localFilePath, bucketName, folderPrefix string) (string, error) {
	ctx := context.Background()
	client, err := storage.NewClient(ctx)
	if err != nil {
		return "", nil
	}
	defer client.Close()

	objectName := fmt.Sprintf("%s/%s", folderPrefix, filepath.Base(localFilePath))
	bucket := client.Bucket(bucketName)
	object := bucket.Object(objectName)

	// FIXME: we should be using a mpu with the signed url below
	/*
		urlExpiration := time.Now().Add(60 * time.Minute)
		url, err := storage.SignedURL(bucketName, objectName, &storage.SignedURLOptions{
			Method:  "PUT",
			Expires: urlExpiration,
		})
		if err != nil {
			return err
		}
	*/

	file, err := os.Open(localFilePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return "", err
	}

	p := mpb.New(mpb.WithWidth(60), mpb.WithRefreshRate(180*time.Millisecond))
	bar := p.AddBar(fileInfo.Size(),
		mpb.PrependDecorators(
			decor.Name("Uploading: "),
			decor.CountersKibiByte("% .2f / % .2f"),
		),
		mpb.AppendDecorators(
			decor.Percentage(),
			decor.EwmaSpeed(decor.UnitKiB, "% .2f", 60),
		),
	)

	const chunkSize = 25 * 1024 * 1024 // 25MB chunk size
	totalChunks := int(fileInfo.Size() / chunkSize)
	if fileInfo.Size()%chunkSize != 0 {
		totalChunks++ // Account for last partial chunk
	}

	chunk := make([]byte, chunkSize)
	wc := object.NewWriter(ctx)
	for i := 0; i < totalChunks; i++ {
		n, err := file.Read(chunk)
		if err != nil && err != io.EOF {
			return "", err
		}

		_, err = wc.Write(chunk[:n])
		if err != nil {
			return "", err
		}

		// Update progress bar after each chunk is uploaded
		bar.IncrBy(n)
	}
	err = wc.Close()
	if err != nil {
		return "", err
	}

	p.Wait() // Wait for the progress bar to finish
	fmt.Println("Upload completed successfully.")
	url := fmt.Sprintf("https://weights.replicate.delivery/wqzt/%s", objectName)
	return url, nil
}

func writeManifest(urls []string, filepaths []string) error {
	if len(urls) != len(filepaths) {
		return fmt.Errorf("urls and filepaths slices must have the same length")
	}

	file, err := os.Create("manifest.pget")
	if err != nil {
		return err
	}
	defer file.Close()

	for i := range urls {
		line := fmt.Sprintf("%s %s\n", urls[i], filepaths[i])
		if _, err := file.WriteString(line); err != nil {
			return err
		}
	}

	return nil
}

var PGET_PY = `# Python Utility for PGet ⥥
# https://github.com/replicate/pget
# Use this script with a pget manifest file:
# from pget import pget_manifest
# pget_manifest('manifest.pget')

# Your manifest file must be in shape:
# https://example.com/image1.jpg /local/path/to/image1.jpg
# https://example.com/document.pdf /local/path/to/document.pdf
# https://example.com/weights.pth /local/path/to/weights.pth
# ... etc ..

# Read more about pget multifile downloads here:
# https://github.com/replicate/pget?tab=readme-ov-file#multi-file-mode

import os
import subprocess
import time

def pget_manifest(manifest_filename: str='manifest.pget'):
  start = time.time()
  with open(manifest_filename, 'r') as f:
    manifest = f.read()


  # ensure directories exist
  for line in manifest.splitlines():
    _, path = line.split(" ")
    os.makedirs(os.path.dirname(path), exist_ok=True)

  # download using pget
  subprocess.check_call(["pget", "multifile", manifest_filename])

  # log metrics
  timing = time.time() - start
  print(f"Downloaded weights in {timing} seconds")`

func writePgetPy() error {
	file, err := os.Create("pget.py")
	if err != nil {
		return err
	}
	defer file.Close()

	if _, err := file.WriteString(PGET_PY); err != nil {
		return err
	}

	return nil
}

type BarebonesCog struct {
	Run []string `yaml:"run,omitempty"`
}

func installPGet() error {
	var config BarebonesCog

	// Read the existing cog.yaml file
	data, err := ioutil.ReadFile("cog.yaml")
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	if err == nil {
		// File exists, unmarshal its content
		if err := yaml.Unmarshal(data, &config); err != nil {
			return err
		}
	}

	// Update or set the run block
	config.Run = []string{"curl -o /usr/local/bin/pget -L \"https://github.com/replicate/pget/releases/latest/download/pget_$(uname -s)_$(uname -m)\"", "chmod +x /usr/local/bin/pget"}

	// Marshal the modified structure back to YAML
	updatedData, err := yaml.Marshal(&config)
	if err != nil {
		return err
	}

	if err := os.WriteFile("cog.yaml", updatedData, 0o755); err != nil {
		return err
	}

	return nil
}
