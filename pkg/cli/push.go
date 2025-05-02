package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/replicate/go/uuid"

	"github.com/replicate/cog/pkg/coglog"
	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/http"
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
	addLocalImage(cmd)
	addConfigFlag(cmd)

	return cmd
}

func push(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	command := docker.NewDockerCommand()
	client, err := http.ProvideHTTPClient(ctx, command)
	if err != nil {
		return err
	}
	logClient := coglog.NewClient(client)
	logCtx := logClient.StartPush(buildFast, buildLocalImage)

	cfg, projectDir, err := config.GetConfig(configFilename)
	if err != nil {
		logClient.EndPush(ctx, err, logCtx)
		return err
	}
	if cfg.Build.Fast {
		buildFast = cfg.Build.Fast
	}

	imageName := cfg.Image
	if len(args) > 0 {
		imageName = args[0]
	}

	if imageName == "" {
		err = fmt.Errorf("To push images, you must either set the 'image' option in cog.yaml or pass an image name as an argument. For example, 'cog push r8.im/your-username/hotdog-detector'")
		logClient.EndPush(ctx, err, logCtx)
		return err
	}

	replicatePrefix := fmt.Sprintf("%s/", global.ReplicateRegistryHost)
	if strings.HasPrefix(imageName, replicatePrefix) {
		if err := docker.ManifestInspect(ctx, imageName); err != nil && strings.Contains(err.Error(), `"code":"NAME_UNKNOWN"`) {
			err = fmt.Errorf("Unable to find Replicate existing model for %s. Go to replicate.com and create a new model before pushing.", imageName)
			logClient.EndPush(ctx, err, logCtx)
			return err
		}
	} else {
		if buildLocalImage {
			err = fmt.Errorf("Unable to push a local image model to a non replicate host, please disable the local image flag before pushing to this host.")
			logClient.EndPush(ctx, err, logCtx)
			return err
		}
	}

	annotations := map[string]string{}
	buildID, err := uuid.NewV7()
	if err != nil {
		// Don't insert build ID but continue anyways
		console.Debugf("Failed to create build ID %v", err)
	} else {
		annotations["run.cog.push_id"] = buildID.String()
	}

	startBuildTime := time.Now()

	if err := image.Build(ctx, cfg, projectDir, imageName, buildSecrets, buildNoCache, buildSeparateWeights, buildUseCudaBaseImage, buildProgressOutput, buildSchemaFile, buildDockerfileFile, DetermineUseCogBaseImage(cmd), buildStrip, buildPrecompile, buildFast, annotations, buildLocalImage, command); err != nil {
		return err
	}

	buildDuration := time.Since(startBuildTime)

	console.Infof("\nPushing image '%s'...", imageName)
	if buildFast {
		console.Info("Fast push enabled.")
	}

	err = docker.Push(ctx, imageName, buildFast, projectDir, command, docker.BuildInfo{
		BuildTime: buildDuration,
		BuildID:   buildID.String(),
	}, client)
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			err = fmt.Errorf("Unable to find existing Replicate model for %s. "+
				"Go to replicate.com and create a new model before pushing."+
				"\n\n"+
				"If the model already exists, you may be getting this error "+
				"because you're not logged in as owner of the model. "+
				"This can happen if you did `sudo cog login` instead of `cog login` "+
				"or `sudo cog push` instead of `cog push`, "+
				"which causes Docker to use the wrong Docker credentials.",
				imageName)
			logClient.EndPush(ctx, err, logCtx)
			return err
		}
		err = fmt.Errorf("Failed to push image: %w", err)
		logClient.EndPush(ctx, err, logCtx)
		return err
	}

	console.Infof("Image '%s' pushed", imageName)
	if strings.HasPrefix(imageName, replicatePrefix) {
		replicatePage := fmt.Sprintf("https://%s", strings.Replace(imageName, global.ReplicateRegistryHost, global.ReplicateWebsiteHost, 1))
		console.Infof("\nRun your model on Replicate:\n    %s", replicatePage)
	}
	logClient.EndPush(ctx, nil, logCtx)

	return nil
}
