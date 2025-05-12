package command

import (
	"context"
	"io"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
)

type Command interface {
	// Pull pulls an image from a remote registry and returns the inspect response for the local image.
	// If the image already exists, it will return the inspect response for the local image without pulling.
	// When force is true, it will always attempt to pull the image.
	Pull(ctx context.Context, ref string, force bool) (*image.InspectResponse, error)
	Push(ctx context.Context, ref string) error
	LoadUserInformation(ctx context.Context, registryHost string) (*UserInfo, error)
	CreateTarFile(ctx context.Context, ref string, tmpDir string, tarFile string, folder string) (string, error)
	CreateAptTarFile(ctx context.Context, tmpDir string, aptTarFile string, packages ...string) (string, error)
	Inspect(ctx context.Context, ref string) (*image.InspectResponse, error)
	ImageExists(ctx context.Context, ref string) (bool, error)
	ContainerLogs(ctx context.Context, containerID string, w io.Writer) error
	ContainerInspect(ctx context.Context, id string) (*container.InspectResponse, error)
	ContainerStop(ctx context.Context, containerID string) error

	ImageBuild(ctx context.Context, options ImageBuildOptions) error
	Run(ctx context.Context, options RunOptions) error
	ContainerStart(ctx context.Context, options RunOptions) (string, error)
}

type ImageBuildOptions struct {
	// Platform           string
	WorkingDir         string
	DockerfileContents string
	ImageName          string
	Secrets            []string
	NoCache            bool
	ProgressOutput     string
	Epoch              *int64
	ContextDir         string
	BuildContexts      map[string]string
	Labels             map[string]string
}

type RunOptions struct {
	Detach  bool
	Args    []string
	Env     []string
	GPUs    string
	Image   string
	Ports   []Port
	Volumes []Volume
	Workdir string
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
}

type Port struct {
	HostPort      int
	ContainerPort int
}

type Volume struct {
	Source      string
	Destination string
}
