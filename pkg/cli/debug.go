package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/global"
)

func newDebugCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "debug",
		Hidden: true,
		RunE:   cmdDockerfile,
	}

	debug := &cobra.Command{
		Use:    "debug",
		Short:  "Generate a Dockerfile from " + global.ConfigFilename,
		Hidden: true,
	}
	cmd.Flags().StringP("arch", "a", "cpu", "Architecture (cpu/gpu)")

	cmd.AddCommand(debug)

	return cmd
}

func cmdDockerfile(cmd *cobra.Command, args []string) error {
	config, projectDir, err := getConfig()
	if err != nil {
		return err
	}

	arch, err := cmd.Flags().GetString("arch")
	if err != nil {
		return err
	}
	generator := docker.NewDockerfileGenerator(config, arch, projectDir)
	out, err := generator.Generate()
	if err != nil {
		return err
	}
	defer generator.Cleanup()
	fmt.Print(out)
	return nil
}
