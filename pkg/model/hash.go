package model

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
)

// hashFile computes SHA256 digest and size of a file by streaming.
func hashFile(path string) (digest string, size int64, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()

	h := sha256.New()
	size, err = io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}

	digest = "sha256:" + hex.EncodeToString(h.Sum(nil))
	return digest, size, nil
}
