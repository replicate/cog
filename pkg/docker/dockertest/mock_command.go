package dockertest

import (
	"context"
	"os"
	"path/filepath"

	"github.com/replicate/cog/pkg/docker/command"
)

var PushError error = nil
var MockCogConfig string = "{\"build\":{\"python_version\":\"3.12\",\"python_packages\":[\"torch==2.5.0\",\"beautifulsoup4==4.12.3\"],\"system_packages\":[\"git\"]},\"image\":\"test\",\"predict\":\"predict.py:Predictor\"}"
var MockOpenAPISchema string = "{}"

type MockCommand struct{}

func NewMockCommand() *MockCommand {
	return &MockCommand{}
}

func (c *MockCommand) Pull(ctx context.Context, image string) error {
	return nil
}

func (c *MockCommand) Push(ctx context.Context, image string) error {
	return PushError
}

func (c *MockCommand) LoadUserInformation(ctx context.Context, registryHost string) (*command.UserInfo, error) {
	userInfo := command.UserInfo{
		Token:    "",
		Username: "",
	}
	return &userInfo, nil
}

func (c *MockCommand) CreateTarFile(ctx context.Context, image string, tmpDir string, tarFile string, folder string) (string, error) {
	path := filepath.Join(tmpDir, tarFile)
	d1 := []byte("hello\ngo\n")
	err := os.WriteFile(path, d1, 0o644)
	if err != nil {
		return "", err
	}
	return path, nil
}

func (c *MockCommand) CreateAptTarFile(ctx context.Context, tmpDir string, aptTarFile string, packages ...string) (string, error) {
	path := filepath.Join(tmpDir, aptTarFile)
	d1 := []byte("hello\ngo\n")
	err := os.WriteFile(path, d1, 0o644)
	if err != nil {
		return "", err
	}
	return path, nil
}

func (c *MockCommand) Inspect(ctx context.Context, image string) (*command.Manifest, error) {
	manifest := command.Manifest{
		Config: command.Config{
			Labels: map[string]string{
				command.CogConfigLabelKey:        MockCogConfig,
				command.CogOpenAPISchemaLabelKey: MockOpenAPISchema,
				command.CogVersionLabelKey:       "0.11.3",
			},
			Env: []string{
				command.R8TorchVersionEnvVarName + "=2.5.0",
				command.R8CudaVersionEnvVarName + "=2.4",
				command.R8CudnnVersionEnvVarName + "=1.0",
				command.R8PythonVersionEnvVarName + "=3.12",
			},
		},
	}
	return &manifest, nil
}
