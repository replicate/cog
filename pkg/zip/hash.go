package zip

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"fmt"
)

func getFileHash(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("Failed to open %s: %v", path, err)
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("Failed to hash file %s: %v", path, err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
