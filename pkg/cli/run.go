package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/logger"
	"github.com/replicate/cog/pkg/util/terminal"
	"github.com/spf13/cobra"
)

func newRunCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run <command> [arg...]",
		Short: "Run a command inside a Docker environment",
		RunE:  run,
		Args:  cobra.MinimumNArgs(1),
	}

	flags := cmd.Flags()
	// Flags after first argment are considered args and passed to command
	flags.SetInterspersed(false)

	return cmd
}

func run(cmd *cobra.Command, args []string) error {
	arch := "cpu"

	ui := terminal.ConsoleUI(context.Background())
	defer ui.Close()

	config, projectDir, err := getConfig()
	if err != nil {
		return err
	}
	logWriter := logger.NewTerminalLogger(ui, "Building Docker image from environment in cog.yaml... ")
	generator := docker.NewDockerfileGenerator(config, arch, projectDir)
	defer func() {
		if err := generator.Cleanup(); err != nil {
			ui.Output(fmt.Sprintf("Error cleaning up Dockerfile generator: %s", err))
		}
	}()
	dockerfileContents, err := generator.GenerateBase()
	if err != nil {
		return fmt.Errorf("Failed to generate Dockerfile for %s: %w", arch, err)
	}
	dockerImageBuilder := docker.NewLocalImageBuilder("")
	buildUseGPU := config.Environment.BuildRequiresGPU && arch == "gpu"
	tag, err := dockerImageBuilder.Build(context.Background(), projectDir, dockerfileContents, "", buildUseGPU, logWriter)
	if err != nil {
		return fmt.Errorf("Failed to build Docker image: %w", err)
	}

	logWriter.Done()

	// TODO(bfirsh): ports
	ports := []string{}

	ui.Output(fmt.Sprintf("Running '%s' in Docker with the current directory mounted as a volume...", strings.Join(args, " ")))
	ui.HorizontalRule()

	dockerArgs := []string{
		"run",
		"--interactive",
		"--rm",
		"--shm-size", "8G", // https://github.com/pytorch/pytorch/issues/2244
		// TODO: escape
		"--volume", projectDir + ":/code",
	}
	for _, port := range ports {
		dockerArgs = append(dockerArgs, "-p", port+":"+port)
	}
	if isatty.IsTerminal(os.Stdin.Fd()) {
		dockerArgs = append(dockerArgs, "--tty")
	}
	dockerArgs = append(dockerArgs, tag)
	dockerArgs = append(dockerArgs, args...)

	dockerCmd := exec.Command("docker", dockerArgs...)
	dockerCmd.Env = os.Environ()
	dockerCmd.Stdout = os.Stdout
	dockerCmd.Stderr = os.Stderr
	dockerCmd.Stdin = os.Stdin

	return dockerCmd.Run()
}
