package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/replicate/go/uuid"

	"github.com/replicate/cog/pkg/coglog"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/http"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/provider"
	"github.com/replicate/cog/pkg/provider/setup"
	"github.com/replicate/cog/pkg/registry"
	"github.com/replicate/cog/pkg/util/console"
)

func newPushCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use: "push [IMAGE]",

		Short:   "Build and push model in current directory to a Docker registry",
		Example: `cog push registry.example.com/your-username/model-name`,
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
	addConfigFlag(cmd)

	return cmd
}

func push(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Initialize the provider registry
	setup.Init()

	dockerClient, err := docker.NewClient(ctx)
	if err != nil {
		return err
	}

	client, err := http.ProvideHTTPClient(ctx, dockerClient)
	if err != nil {
		return err
	}

	src, err := model.NewSource(configFilename)
	if err != nil {
		return err
	}

	// In case one of `--x-fast` & `fast: bool` is set
	if src.Config.Build != nil && src.Config.Build.Fast {
		buildFast = true
	}

	imageName := src.Config.Image
	if len(args) > 0 {
		imageName = args[0]
	}

	if imageName == "" {
		return fmt.Errorf("To push images, you must either set the 'image' option in cog.yaml or pass an image name as an argument. For example, 'cog push registry.example.com/your-username/model-name'")
	}

	// Look up the provider for the target registry
	p := provider.DefaultRegistry().ForImage(imageName)
	isReplicate := p != nil && p.Name() == "replicate"

	// Helper function for analytics - only logs when pushing to Replicate
	var logClient *coglog.Client
	var logCtx coglog.PushLogContext
	if isReplicate {
		logClient = coglog.NewClient(client)
		logCtx = logClient.StartPush()
		logCtx.Fast = buildFast
		logCtx.CogRuntime = false
		if src.Config.Build != nil && src.Config.Build.CogRuntime != nil {
			logCtx.CogRuntime = *src.Config.Build.CogRuntime
		}
	}

	// Wrapper to conditionally log errors
	endPushWithError := func(err error) error {
		if isReplicate && logClient != nil {
			logClient.EndPush(ctx, err, logCtx)
		}
		return err
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
	resolver := model.NewResolver(dockerClient, registry.NewRegistryClient())
	m, err := resolver.Build(ctx, src, buildOptionsFromFlags(cmd, imageName, buildFast, annotations))
	if err != nil {
		return endPushWithError(err)
	}

	buildDuration := time.Since(startBuildTime)

	console.Infof("\nPushing image '%s'...", m.ImageRef())
	if buildFast {
		if isReplicate {
			console.Info("Fast push enabled.")
		} else {
			console.Warnf("Fast push (--x-fast) is only supported for Replicate's registry (%s). Falling back to standard push.", global.ReplicateRegistryHost)
			buildFast = false
		}
	}

	err = docker.Push(ctx, m.ImageRef(), buildFast, src.ProjectDir, dockerClient, docker.BuildInfo{
		BuildTime: buildDuration,
		BuildID:   buildID.String(),
	}, client, src.Config)
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			if isReplicate {
				// Replicate-specific error message with helpful hints
				err = fmt.Errorf("Unable to find existing Replicate model for %s. "+
					"Go to replicate.com and create a new model before pushing."+
					"\n\n"+
					"If the model already exists, you may be getting this error "+
					"because you're not logged in as owner of the model. "+
					"This can happen if you did `sudo cog login` instead of `cog login` "+
					"or `sudo cog push` instead of `cog push`, "+
					"which causes Docker to use the wrong Docker credentials.",
					imageName)
			} else {
				// Generic error message for other registries
				err = fmt.Errorf("Failed to push image %s: repository not found (404). "+
					"Please ensure the repository exists and you have push access. "+
					"You may need to run 'docker login' to authenticate.",
					imageName)
			}
			return endPushWithError(err)
		}
		err = fmt.Errorf("Failed to push image: %w", err)
		return endPushWithError(err)
	}

	console.Infof("Image '%s' pushed", imageName)
	if isReplicate {
		replicatePage := fmt.Sprintf("https://%s", strings.Replace(imageName, global.ReplicateRegistryHost, global.ReplicateWebsiteHost, 1))
		console.Infof("\nRun your model on Replicate:\n    %s", replicatePage)
		logClient.EndPush(ctx, nil, logCtx)
	}

	return nil
}
