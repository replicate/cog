package util

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
)

func SHA256HashFile(path string) (string, error) {
	hash := sha256.New()

	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}
