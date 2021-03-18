package cli

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"

	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/server"
	"github.com/spf13/cobra"
)

func newDebugCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "debug",
		Hidden: true,
		RunE:   cmdDockerfile,
	}

	debug := &cobra.Command{
		Use:    "debug",
		Short:  "Generate a Dockerfile from cog.yaml",
		Hidden: true,
	}

	cmd.AddCommand(debug)

	return cmd
}

func cmdDockerfile(cmd *cobra.Command, args []string) error {
	projectDir, err := os.Getwd()
	if err != nil {
		return err
	}

	configPath := path.Join(projectDir, "cog.yaml")

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return fmt.Errorf("cog.yaml does not exist in %s. Are you in the right directory?", projectDir)
	}

	contents, err := ioutil.ReadFile(configPath)
	if err != nil {
		return err
	}

	config, err := model.ConfigFromYAML(contents)
	if err != nil {
		return err
	}

	generator := &server.DockerfileGenerator{Config: config, Arch: "cpu"}
	out, err := generator.Generate()
	if err != nil {
		return err
	}
	fmt.Print(out)
	return nil
}
