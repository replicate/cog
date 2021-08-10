package cli

import (
	"strings"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/image"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/spf13/cobra"
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
	flags.SetInterspersed(false)

	return cmd
}

func run(cmd *cobra.Command, args []string) error {
	cfg, projectDir, err := config.GetConfig(projectDirFlag)
	if err != nil {
		return err
	}

	imageName, err := image.BuildBase(cfg, projectDir, buildProgressOutput)
	if err != nil {
		return err
	}

	gpus := ""
	if cfg.Build.GPU {
		gpus = "all"
	}

	console.Info("")
	console.Infof("Running '%s' in Docker with the current directory mounted as a volume...", strings.Join(args, " "))
	return docker.Run(docker.RunOptions{
		Args:    args,
		GPUs:    gpus,
		Image:   imageName,
		Volumes: []docker.Volume{{Source: projectDir, Destination: "/src"}},
		Workdir: "/src",
	})
}
