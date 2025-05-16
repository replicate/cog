package weights

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/replicate/cog/pkg/dockerignore"
	"github.com/replicate/cog/pkg/util"
)

var WEIGHT_FILE_EXCLUSIONS = []string{
	".gif",
	".ipynb",
	".jpeg",
	".jpg",
	".log",
	".mp4",
	".png",
	".svg",
	".webp",
}
var WEIGHT_FILE_INCLUSIONS = []string{
	".ckpt",
	".h5",
	".onnx",
	".pb",
	".pbtxt",
	".pt",
	".pth",
	".safetensors",
	".tflite",
}

const WEIGHT_FILE_SIZE_EXCLUSION = 1024 * 1024
const WEIGHT_FILE_SIZE_INCLUSION = 128 * 1024 * 1024
const WEIGHT_FILE = "weights.json"

type Weight struct {
	Path      string    `json:"path"`
	Digest    string    `json:"digest"`
	Timestamp time.Time `json:"timestamp"`
	Size      int64     `json:"size"`
}

func FindFastWeights(folder string, tmpDir string) ([]Weight, error) {
	weightFile := filepath.Join(tmpDir, WEIGHT_FILE)
	if _, err := os.Stat(weightFile); errors.Is(err, os.ErrNotExist) {
		return findFullWeights(folder, []Weight{}, weightFile)
	}
	return checkWeights(folder, weightFile)
}

func ReadFastWeights(tmpDir string) ([]Weight, error) {
	return readWeights(filepath.Join(tmpDir, WEIGHT_FILE))
}

func findFullWeights(folder string, weights []Weight, weightFile string) ([]Weight, error) {
	matcher, err := dockerignore.CreateMatcher(folder)
	if err != nil {
		return weights, err
	}
	err = dockerignore.Walk(folder, matcher, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(folder, path)
		if err != nil {
			return err
		}

		for _, weight := range weights {
			if weight.Path == relPath {
				return nil
			}
		}

		ext := filepath.Ext(path)

		if slices.Contains(WEIGHT_FILE_EXCLUSIONS, ext) || info.Size() <= WEIGHT_FILE_SIZE_EXCLUSION {
			return nil
		}

		if slices.Contains(WEIGHT_FILE_INCLUSIONS, ext) || info.Size() >= WEIGHT_FILE_SIZE_INCLUSION {
			hash, err := util.SHA256HashFile(path)
			if err != nil {
				return err
			}

			weights = append(weights, Weight{
				Path:      relPath,
				Digest:    hash,
				Timestamp: info.ModTime(),
				Size:      info.Size(),
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	jsonData, err := json.MarshalIndent(weights, "", "  ")
	if err != nil {
		return nil, err
	}
	err = os.WriteFile(weightFile, jsonData, 0o644)
	if err != nil {
		return nil, err
	}

	return weights, err
}

func checkWeights(folder string, weightFile string) ([]Weight, error) {
	weights, err := readWeights(weightFile)
	if err != nil {
		return nil, err
	}

	newWeights := []Weight{}
	for _, weight := range weights {
		info, err := os.Stat(weight.Path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}

		if weight.Timestamp != info.ModTime() || weight.Size != info.Size() {
			continue
		}
		newWeights = append(newWeights, weight)
	}

	return findFullWeights(folder, newWeights, weightFile)
}

func readWeights(weightFile string) ([]Weight, error) {
	var weights []Weight

	if _, err := os.Stat(weightFile); errors.Is(err, os.ErrNotExist) {
		return weights, nil
	}

	file, err := os.Open(weightFile)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	err = decoder.Decode(&weights)
	if err != nil {
		return nil, err
	}

	return weights, nil
}
