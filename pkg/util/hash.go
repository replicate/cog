package util

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
)

var (
	ErrInvalidRange = errors.New("Invalid byte range provided for file")
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

func SHA256HashFileWithSaltAndRange(path string, start int, end int, salt string) (string, error) {
	hash := sha256.New()
	length := end - start

	if length < 0 {
		return "", ErrInvalidRange
	}

	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return "", err
	}

	if fileInfo.Size() < int64(end) {
		return "", ErrInvalidRange
	}

	_, err = file.Seek(int64(start), 0)
	if err != nil {
		return "", fmt.Errorf("failed to open file pointer %s: %w", path, err)
	}
	buf := make([]byte, length)
	n, err := file.Read(buf)
	if err != nil {
		return "", err
	}

	buf = buf[:n]
	var hashInput []byte
	hashInput = append(hashInput, buf...)
	hashInput = append(hashInput, []byte(salt)...)

	if _, err := io.Copy(hash, bytes.NewReader(hashInput)); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}
