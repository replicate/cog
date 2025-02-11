package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/image"
	"github.com/replicate/cog/pkg/util/console"
)

func newPushCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use: "push [IMAGE]",

		Short:   "Build and push model in current directory to a Docker registry",
		Example: `cog push r8.im/your-username/hotdog-detector`,
		RunE:    push,
		Args:    cobra.MaximumNArgs(1),
	}
	addSecretsFlag(cmd)
	addNoCacheFlag(cmd)
	addSeparateWeightsFlag(cmd)
	addSchemaFlag(cmd)
	addUseCudaBaseImageFlag(cmd)
	addDockerfileFlag(cmd)
	addBuildProgressOutputFlag(cmd)
	addUseCogBaseImageFlag(cmd)
	addStripFlag(cmd)
	addPrecompileFlag(cmd)
	addFastFlag(cmd)

	return cmd
}

func push(cmd *cobra.Command, args []string) error {
	cfg, projectDir, err := config.GetConfig(projectDirFlag)
	if err != nil {
		return err
	}

	imageName := cfg.Image
	if len(args) > 0 {
		imageName = args[0]
	}

	if imageName == "" {
		return fmt.Errorf("To push images, you must either set the 'image' option in cog.yaml or pass an image name as an argument. For example, 'cog push r8.im/your-username/hotdog-detector'")
	}

	replicatePrefix := fmt.Sprintf("%s/", global.ReplicateRegistryHost)
	if strings.HasPrefix(imageName, replicatePrefix) {
		if err := docker.ManifestInspect(imageName); err != nil && strings.Contains(err.Error(), `"code":"NAME_UNKNOWN"`) {
			return fmt.Errorf("Unable to find Replicate existing model for %s. Go to replicate.com and create a new model before pushing.", imageName)
		}
	}

	if err := image.Build(cfg, projectDir, imageName, buildSecrets, buildNoCache, buildSeparateWeights, buildUseCudaBaseImage, buildProgressOutput, buildSchemaFile, buildDockerfileFile, DetermineUseCogBaseImage(cmd), buildStrip, buildPrecompile, buildFast); err != nil {
		return err
	}

	console.Infof("\nPushing image '%s'...", imageName)
	if buildFast {
		console.Info("Fast push enabled.")
	}

	command := docker.NewDockerCommand()
	err = docker.Push(imageName, buildFast, projectDir, command)
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			return fmt.Errorf("Unable to find existing Replicate model for %s. "+
				"Go to replicate.com and create a new model before pushing."+
				"\n\n"+
				"If the model already exists, you may be getting this error "+
				"because you're not logged in as owner of the model. "+
				"This can happen if you did `sudo cog login` instead of `cog login` "+
				"or `sudo cog push` instead of `cog push`, "+
				"which causes Docker to use the wrong Docker credentials.",
				imageName)
		}
		return fmt.Errorf("Failed to push image: %w", err)
	}

	console.Infof("Image '%s' pushed", imageName)
	if strings.HasPrefix(imageName, replicatePrefix) {
		replicatePage := fmt.Sprintf("https://%s", strings.Replace(imageName, global.ReplicateRegistryHost, global.ReplicateWebsiteHost, 1))
		console.Infof("\nRun your model on Replicate:\n    %s", replicatePage)
	}

	return nil
}
