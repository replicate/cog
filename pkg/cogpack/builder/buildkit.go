package builder

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"

	buildkitclient "github.com/moby/buildkit/client"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/frontend/gateway/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/tonistiigi/fsutil"
	"golang.org/x/sync/errgroup"

	"github.com/replicate/cog/pkg/cogpack/plan"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/util"
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
func (b *BuildKitBuilder) Build(ctx context.Context, p *plan.Plan, buildConfig *BuildConfig) (string, *ocispec.Image, error) {
	fmt.Println("Building with BuildKit")

	// Get BuildKit client from Docker command
	bkClient, err := b.dockerCmd.BuildKitClient(ctx)
	if err != nil {
		return "", nil, fmt.Errorf("get buildkit client: %w", err)
	}
	defer bkClient.Close()

	info, err := bkClient.Info(ctx)
	fmt.Println("BuildKit info")
	util.JSONPrettyPrint(info)

	if err != nil {
		return "", nil, fmt.Errorf("get buildkit client: %w", err)
	}
	defer bkClient.Close()
	fmt.Println("BuildKit client obtained")
	contextFS, err := fsutil.NewFS(buildConfig.ContextDir)
	if err != nil {
		return "", nil, fmt.Errorf("context fs: %w", err)
	}
	fmt.Println("Context FS obtained")

	// Create contexts from plan
	localMounts := map[string]fsutil.FS{
		"context": contextFS,
	}

	// Track contexts for cleanup
	var contextCleanupFuncs []func() error
	defer func() {
		for _, cleanup := range contextCleanupFuncs {
			cleanup()
		}
	}()

	// Process plan contexts generically
	for name, buildCtx := range p.Contexts {
		fsutilFS, err := convertToFsutilFS(buildCtx.FS)
		if err != nil {
			return "", nil, fmt.Errorf("convert context %s (%s): %w", name, buildCtx.Description, err)
		}
		localMounts[name] = fsutilFS

		// Debug logging
		fmt.Printf("Added context: %s - %s (from %s)\n", name, buildCtx.Description, buildCtx.SourceBlock)
		for k, v := range buildCtx.Metadata {
			fmt.Printf("  %s: %s\n", k, v)
		}
	}

	solveOpt := buildkitclient.SolveOpt{
		Exports: []buildkitclient.ExportEntry{{
			Type: "moby",
			Attrs: map[string]string{
				"name": buildConfig.Tag,
			},
		}},
		LocalMounts: localMounts,
	}
	fmt.Println("Solve options created")

	// Create a status channel for build progress
	statusCh := make(chan *buildkitclient.SolveStatus)

	productID := fmt.Sprintf("cogpack-model:%s", buildConfig.Tag)

	eg, egctx := errgroup.WithContext(ctx)
	eg.Go(docker.NewBuildkitSolveDisplay(statusCh, "plain"))

	var solveResp *buildkitclient.SolveResponse
	var resultImage *ocispec.Image

	eg.Go(func() error {
		resp, err := bkClient.Build(
			egctx,
			solveOpt,
			productID,
			func(ctx context.Context, c client.Client) (*client.Result, error) {
				// Translate plan → llb with gateway client for image config inspection
				finalState, _, err := TranslatePlan(ctx, p, c)
				if err != nil {
					return nil, fmt.Errorf("plan translation failed: %w", err)
				}
				fmt.Println("Plan translated")
				def, err := finalState.Marshal(ctx)
				if err != nil {
					return nil, fmt.Errorf("marshal llb: %w", err)
				}
				fmt.Println("LLB marshalled")

				res, err := c.Solve(ctx, client.SolveRequest{Definition: def.ToPB()})
				if err != nil {
					return nil, fmt.Errorf("solve request failed: %w", err)
				}

				fmt.Println("Solve request completed")

				img, err := ociImageFromStateAndPlan(ctx, finalState, p)
				if err != nil {
					return nil, fmt.Errorf("failed to create image config: %w", err)
				}

				resultImage = img

				cfgJSON, err := json.Marshal(img)
				if err != nil {
					return nil, fmt.Errorf("marshal image config: %w", err)
				}
				fmt.Println("Image config marshalled")
				out := &client.Result{}
				out.SetRef(res.Ref)
				out.AddMeta("containerimage.config", cfgJSON)
				return out, nil
			},
			statusCh,
		)
		if err != nil {
			return fmt.Errorf("failed to solve build: %w", err)
		}
		solveResp = resp
		return nil
	})

	if err := eg.Wait(); err != nil {
		return "", nil, fmt.Errorf("failed to solve build: %w", err)
	}

	fmt.Println("solveResp")
	util.JSONPrettyPrint(solveResp)

	return solveResp.ExporterResponse["image.name"], resultImage, nil
}

func ociImageFromStateAndPlan(ctx context.Context, finalState llb.State, p *plan.Plan) (*ocispec.Image, error) {
	img := &ocispec.Image{
		Config: ocispec.ImageConfig{},
		Author: "cogpack",
	}

	// platform
	if platform, err := finalState.GetPlatform(ctx); err == nil {
		img.Platform = *platform
	} else {
		return nil, fmt.Errorf("error extracting platform from final state: %w", err)
	}

	// env
	if envList, err := finalState.Env(ctx); err == nil {
		img.Config.Env = envList.ToArray()
	} else {
		return nil, fmt.Errorf("error extrating env from final state: %w", err)
	}

	// working dir
	if dir, err := finalState.GetDir(ctx); err == nil {
		img.Config.WorkingDir = dir
	} else {
		return nil, fmt.Errorf("error extracting working dir from final state: %w", err)
	}

	// TODO: make sure entrypoint etc are applied from the base image!
	if p.Export != nil {
		img.Config.Entrypoint = p.Export.Entrypoint
		img.Config.Cmd = p.Export.Cmd
		// TDOO: get user from state!
		img.Config.User = p.Export.User
		img.Config.Labels = p.Export.Labels
		img.Config.ExposedPorts = p.Export.ExposedPorts
	}

	return img, nil
}

// convertToFsutilFS converts fs.FS to fsutil.FS
func convertToFsutilFS(filesystem fs.FS) (fsutil.FS, error) {
	// TODO: Check if already fsutil.FS to avoid temp dir conversion
	// For now, use simplest approach
	contextFS, err := NewContextFromFS("temp", filesystem)
	if err != nil {
		return nil, err
	}
	return contextFS.FS(), nil
}
