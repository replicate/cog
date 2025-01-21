package docker

import (
	"os"
	"path/filepath"
)

var PushError error = nil

type MockCommand struct{}

func NewMockCommand() *MockCommand {
	return &MockCommand{}
}

func (c *MockCommand) Push(image string) error {
	return PushError
}

func (c *MockCommand) LoadLoginToken(registryHost string) (string, error) {
	return "", nil
}

func (c *MockCommand) CreateTarFile(image string, tmpDir string, tarFile string, folder string) (string, error) {
	path := filepath.Join(tmpDir, tarFile)
	d1 := []byte("hello\ngo\n")
	err := os.WriteFile(path, d1, 0o644)
	if err != nil {
		return "", err
	}
	return path, nil
}
