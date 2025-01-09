package dockerfile

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"slices"
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

func FindWeights(folder string) (map[string]string, error) {
	weights := make(map[string]string)

	err := filepath.Walk(folder, func(path string, info os.FileInfo, err error) error {
		ext := filepath.Ext(path)

		if slices.Contains(WEIGHT_FILE_EXCLUSIONS, ext) || info.Size() <= WEIGHT_FILE_SIZE_EXCLUSION {
			return nil
		}

		if slices.Contains(WEIGHT_FILE_INCLUSIONS, ext) || info.Size() >= WEIGHT_FILE_SIZE_INCLUSION {
			hash := sha256.New()

			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()

			if _, err := io.Copy(hash, file); err != nil {
				return err
			}

			weights[hex.EncodeToString(hash.Sum(nil))] = path
		}
		return nil
	})

	return weights, err
}
