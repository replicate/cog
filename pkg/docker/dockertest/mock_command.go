package dockertest

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"

	"github.com/replicate/cog/pkg/docker/command"
)

var PushError error = nil
var MockCogConfig string = "{\"build\":{\"python_version\":\"3.12\",\"python_packages\":[\"torch==2.5.0\",\"beautifulsoup4==4.12.3\"],\"system_packages\":[\"git\"]},\"image\":\"test\",\"predict\":\"predict.py:Predictor\"}"
var MockOpenAPISchema string = "{}"

type MockCommand struct{}

func NewMockCommand() *MockCommand {
	return &MockCommand{}
}

func (c *MockCommand) Pull(ctx context.Context, image string, force bool) (*image.InspectResponse, error) {
	return nil, nil
}

func (c *MockCommand) Push(ctx context.Context, image string) error {
	return PushError
}

func (c *MockCommand) LoadUserInformation(ctx context.Context, registryHost string) (*command.UserInfo, error) {
	userInfo := command.UserInfo{
		Token:    "test-token",
		Username: "test-user",
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

func (c *MockCommand) Inspect(ctx context.Context, ref string) (*image.InspectResponse, error) {
	resp := &image.InspectResponse{
		Config: &container.Config{
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

	return resp, nil
}

func (c *MockCommand) ImageExists(ctx context.Context, ref string) (bool, error) {
	panic("not implemented")
}

func (c *MockCommand) ContainerLogs(ctx context.Context, containerID string, w io.Writer) error {
	panic("not implemented")
}

func (c *MockCommand) ContainerInspect(ctx context.Context, id string) (*container.InspectResponse, error) {
	panic("not implemented")
}

func (c *MockCommand) ContainerStop(ctx context.Context, containerID string) error {
	panic("not implemented")
}

func (c *MockCommand) ImageBuild(ctx context.Context, options command.ImageBuildOptions) error {
	panic("not implemented")
}

func (c *MockCommand) Run(ctx context.Context, options command.RunOptions) error {
	// hack to handle generating tar files for monobase
	if options.Args[0] == "/opt/r8/monobase/tar.sh" || options.Args[0] == "/opt/r8/monobase/apt.sh" {
		tmpDir := options.Volumes[0].Source
		tarfile := strings.TrimPrefix(options.Args[1], "/buildtmp/")

		outPath := filepath.Join(tmpDir, tarfile)
		return os.WriteFile(outPath, []byte("hello\ngo\n"), 0o644)
	}

	panic("not implemented")
}

func (c *MockCommand) ContainerStart(ctx context.Context, options command.RunOptions) (string, error) {
	panic("not implemented")
}
