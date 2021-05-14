package cli

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/util/files"
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
	projectDir, err := os.Getwd()
	if err != nil {
		return err
	}

	configPath := path.Join(projectDir, global.ConfigFilename)

	exists, err := files.Exists(configPath)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("%s does not exist in %s. Are you in the right directory?", global.ConfigFilename, projectDir)
	}

	contents, err := ioutil.ReadFile(configPath)
	if err != nil {
		return err
	}

	config, err := model.ConfigFromYAML(contents)
	if err != nil {
		return err
	}
	if err := config.ValidateAndCompleteConfig(); err != nil {
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
