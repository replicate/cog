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

	if err := image.Build(cfg, projectDir, imageName, buildSecrets, buildNoCache, buildSeparateWeights, buildUseCudaBaseImage, buildProgressOutput, buildSchemaFile, buildDockerfileFile, buildUseCogBaseImage); err != nil {
		if buildUseCogBaseImage && cmd.Flags().Changed("use-cog-base-image") {
			console.Infof("Push failed with Cog base image enabled by default. " +
				"If you want to push without using pre-built base images, " +
				"try `cog push --use-cog-base-image=false`.")
		}

		return err
	}

	console.Infof("\nPushing image '%s'...", imageName)

	exitStatus := docker.Push(imageName)
	if exitStatus == nil {
		console.Infof("Image '%s' pushed", imageName)
		if strings.HasPrefix(imageName, replicatePrefix) {
			replicatePage := fmt.Sprintf("https://%s", strings.Replace(imageName, global.ReplicateRegistryHost, global.ReplicateWebsiteHost, 1))
			console.Infof("\nRun your model on Replicate:\n    %s", replicatePage)
		}
	}
	return exitStatus
}
