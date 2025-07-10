package builder

import (
	"context"
	"encoding/json"
	"fmt"

	buildkitclient "github.com/moby/buildkit/client"
	"github.com/moby/buildkit/frontend/gateway/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/tonistiigi/fsutil"

	"github.com/replicate/cog/pkg/cogpack/plan"
	"github.com/replicate/cog/pkg/docker/command"
)

// ensure BuildKitBuilder implements cogpack.Builder
var _ Builder = (*BuildKitBuilder)(nil)

// BuildKitBuilder executes a cogpack.Plan using BuildKit via Docker Engine.
type BuildKitBuilder struct {
	dockerCmd command.Command
}

func NewBuildKitBuilder(dockerCmd command.Command) *BuildKitBuilder {
	return &BuildKitBuilder{dockerCmd: dockerCmd}
}

// Build implements cogpack.Builder.
func (b *BuildKitBuilder) Build(ctx context.Context, plan *plan.Plan, buildContextDir, tag string) error {
	// Translate plan → llb
	finalState, _, err := translatePlan(ctx, plan)
	if err != nil {
		return fmt.Errorf("plan translation failed: %w", err)
	}

	def, err := finalState.Marshal(ctx)
	if err != nil {
		return fmt.Errorf("marshal llb: %w", err)
	}

	// Get BuildKit client from Docker command
	bkClient, err := b.dockerCmd.BuildKitClient(ctx)
	if err != nil {
		return fmt.Errorf("get buildkit client: %w", err)
	}
	defer bkClient.Close()

	contextFS, err := fsutil.NewFS(buildContextDir)
	if err != nil {
		return fmt.Errorf("context fs: %w", err)
	}

	solveOpt := buildkitclient.SolveOpt{
		Exports: []buildkitclient.ExportEntry{{
			Type:  buildkitclient.ExporterImage,
			Attrs: map[string]string{"name": tag, "push": "false"},
		}},
		LocalMounts: map[string]fsutil.FS{"context": contextFS},
	}

	_, err = bkClient.Build(ctx, solveOpt, "cogpack-mvp", func(ctx context.Context, c client.Client) (*client.Result, error) {
		res, err := c.Solve(ctx, client.SolveRequest{Definition: def.ToPB()})
		if err != nil {
			return nil, fmt.Errorf("solve request failed: %w", err)
		}

		imgCfg := ocispec.ImageConfig{}
		if plan.Export != nil {
			imgCfg.Entrypoint = plan.Export.Entrypoint
			imgCfg.Cmd = plan.Export.Cmd
			imgCfg.Env = plan.Export.Env
			imgCfg.WorkingDir = plan.Export.WorkingDir
			imgCfg.User = plan.Export.User
			imgCfg.Labels = plan.Export.Labels
			imgCfg.ExposedPorts = plan.Export.ExposedPorts
		}

		img := ocispec.Image{}
		img.Config = imgCfg
		img.Platform = ocispec.Platform{OS: plan.Platform.OS, Architecture: plan.Platform.Arch}

		cfgJSON, err := json.Marshal(img)
		if err != nil {
			return nil, fmt.Errorf("marshal image config: %w", err)
		}

		out := &client.Result{}
		out.SetRef(res.Ref)
		out.AddMeta("containerimage.config", cfgJSON)
		return out, nil
	}, nil)

	if err != nil {
		return fmt.Errorf("buildkit build failed: %w", err)
	}

	return nil
}
