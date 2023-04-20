package cli

import (
	"strconv"
	"strings"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/image"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/spf13/cobra"
)

var (
	runPorts []string
)

func newRunCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run <command> [arg...]",
		Short: "Run a command inside a Docker environment",
		RunE:  run,
		Args:  cobra.MinimumNArgs(1),
	}
	addBuildProgressOutputFlag(cmd)

	flags := cmd.Flags()
	// Flags after first argment are considered args and passed to command

	// This is called `publish` for consistency with `docker run`
	cmd.Flags().StringArrayVarP(&runPorts, "publish", "p", []string{}, "Publish a container's port to the host, e.g. -p 8000")

	flags.SetInterspersed(false)
	addGroupFileFlag(cmd)

	return cmd
}

func run(cmd *cobra.Command, args []string) error {
	cfg, projectDir, err := config.GetConfig(projectDirFlag)
	if err != nil {
		return err
	}

	imageName, err := image.BuildBase(cfg, projectDir, buildProgressOutput, groupFile)
	if err != nil {
		return err
	}

	gpus := ""
	if cfg.Build.GPU {
		gpus = "all"
	}

	runOptions := docker.RunOptions{
		Args:    args,
		GPUs:    gpus,
		Image:   imageName,
		Volumes: []docker.Volume{{Source: projectDir, Destination: "/src"}},
		Workdir: "/src",
	}

	for _, portString := range runPorts {
		port, err := strconv.Atoi(portString)
		if err != nil {
			return err
		}

		runOptions.Ports = append(runOptions.Ports, docker.Port{HostPort: port, ContainerPort: port})
	}

	console.Info("")
	console.Infof("Running '%s' in Docker with the current directory mounted as a volume...", strings.Join(args, " "))
	return docker.Run(runOptions)
}
