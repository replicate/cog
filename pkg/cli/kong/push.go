package kong

import (
	"fmt"
	"time"

	"github.com/replicate/go/uuid"

	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/http"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/provider"
	"github.com/replicate/cog/pkg/provider/setup"
	"github.com/replicate/cog/pkg/registry"
	"github.com/replicate/cog/pkg/util/console"
)

// PushCmd implements `cog push [IMAGE]`.
type PushCmd struct {
	Image string `arg:"" optional:"" help:"Image name to push to"`

	BuildFlags `embed:""`
}

func (c *PushCmd) Run(g *Globals) error {
	ctx := contextFromGlobals(g)

	setup.Init()

	dockerClient, err := docker.NewClient(ctx)
	if err != nil {
		return err
	}

	httpClient, err := http.ProvideHTTPClient(ctx, dockerClient)
	if err != nil {
		return err
	}

	src, err := model.NewSource(c.ConfigFile)
	if err != nil {
		return err
	}

	imageName := src.Config.Image
	if c.Image != "" {
		imageName = c.Image
	}
	if imageName == "" {
		return fmt.Errorf("To push images, you must either set the 'image' option in cog.yaml or pass an image name as an argument. For example, 'cog push registry.example.com/your-username/model-name'")
	}

	p := provider.DefaultRegistry().ForImage(imageName)
	if p == nil {
		return fmt.Errorf("no provider found for image '%s'", imageName)
	}

	pushOpts := provider.PushOptions{
		Image:      imageName,
		Config:     src.Config,
		ProjectDir: src.ProjectDir,
	}

	buildID, _ := uuid.NewV7()
	annotations := map[string]string{}
	if buildID.String() != "" {
		annotations["run.cog.push_id"] = buildID.String()
	}

	startBuildTime := time.Now()
	regClient := registry.NewRegistryClient()
	resolver := model.NewResolver(dockerClient, regClient)

	buildOpts := c.BuildFlags.BuildOptions(imageName, annotations)
	m, err := resolver.Build(ctx, src, buildOpts)
	if err != nil {
		_ = p.PostPush(ctx, pushOpts, err)
		return err
	}

	buildDuration := time.Since(startBuildTime)

	if m.ImageFormat == model.FormatBundle && m.WeightsManifest != nil {
		console.Infof("\nBundle format: %d weight files (%.2f MB)",
			len(m.WeightsManifest.Files), float64(m.WeightsManifest.TotalSize())/1024/1024)
	}

	console.Infof("\nPushing image '%s'...", m.ImageRef())

	pushErr := docker.Push(ctx, m.ImageRef(), src.ProjectDir, dockerClient, docker.BuildInfo{
		BuildTime: buildDuration,
		BuildID:   buildID.String(),
	}, httpClient)

	if err := p.PostPush(ctx, pushOpts, pushErr); err != nil {
		return err
	}

	if pushErr != nil {
		return fmt.Errorf("failed to push image: %w", pushErr)
	}

	return nil
}
