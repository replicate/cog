package cogpack

import (
	"context"
	"fmt"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/replicate/cog/pkg/cogpack/builder"
	"github.com/replicate/cog/pkg/cogpack/plan"
	"github.com/replicate/cog/pkg/cogpack/project"
	"github.com/replicate/cog/pkg/cogpack/stacks"
	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/util"
)

func NewSourceInfo(rootPath string, config *config.Config) (*project.SourceInfo, error) {
	fs, err := project.NewSourceFS(rootPath)
	if err != nil {
		return nil, err
	}
	return &project.SourceInfo{Config: config, FS: fs}, nil
}

// GeneratePlan generates a complete build plan for the given project.
// This is the main entry point for the cogpack system.
func GeneratePlan(ctx context.Context, src *project.SourceInfo) (*plan.PlanResult, error) {
	// 1. Initialize plan composer with platform
	composer := plan.NewPlanComposer()
	composer.SetPlatform(plan.Platform{
		OS:   "linux",
		Arch: "amd64",
	})

	// Add default build context
	composer.AddContext("context", &plan.BuildContext{
		Name:        "context",
		SourceBlock: "context",
		Description: "Build context",
		Metadata:    map[string]string{},
		FS:          src.FS,
	})

	// 2. Select stack (first match wins)
	stack, err := stacks.SelectStack(ctx, src)
	if err != nil {
		return nil, fmt.Errorf("stack selection failed: %w", err)
	}

	// 3. Let stack orchestrate the build
	if err := stack.Plan(ctx, src, composer); err != nil {
		return nil, fmt.Errorf("stack %s planning failed: %w", stack.Name(), err)
	}

	// 4. Compose the final plan
	p, err := composer.Compose()
	if err != nil {
		return nil, fmt.Errorf("plan composition failed: %w", err)
	}

	// // 5. Validate plan
	// if err := plan.ValidatePlan(p); err != nil {
	// 	return nil, fmt.Errorf("plan validation failed: %w", err)
	// }

	// 6. Create result with metadata
	metadata := &plan.PlanMetadata{
		Stack:   stack.Name(),
		Version: "1.0",
	}

	return &plan.PlanResult{
		Plan:          p,
		Metadata:      metadata,
		Stack:         stack,
		ComposerState: composer.Debug(),
		Timing:        map[string]string{}, // TODO: Add timing information
	}, nil
}

// ExecutePlan executes a pre-generated Plan using the supplied Builder.
func RunBuildPlan(ctx context.Context, provider command.Command, p *plan.Plan, buildCfg *builder.BuildConfig) (string, *ocispec.Image, error) {
	return builder.NewBuildKitBuilder(provider).Build(ctx, p, buildCfg)
}

// // BuildWithDocker is a convenience helper that performs the full pipeline:
// //  1. Source inspection of srcDir into a SourceInfo
// //  2. Plan generation (GeneratePlan)
// //  3. Plan execution via the supplied Builder that uses Docker
// //
// // It returns the Plan so callers can inspect or snapshot it.
// func BuildWithDocker(ctx context.Context, srcDir, tag string, dockerCmd command.Command, builderFactory func(command.Command) builder.Builder) (*plan.Plan, error) {
// 	if dockerCmd == nil {
// 		return nil, fmt.Errorf("docker command cannot be nil")
// 	}
// 	if builderFactory == nil {
// 		return nil, fmt.Errorf("builder factory cannot be nil")
// 	}

// 	// Initialize a basic config with Build field
// 	cfg := &config.Config{
// 		Build: &config.Build{},
// 	}

// 	src, err := NewSourceInfo(srcDir, cfg)
// 	if err != nil {
// 		return nil, fmt.Errorf("new source info: %w", err)
// 	}
// 	defer src.Close()

// 	planResult, err := GeneratePlan(ctx, src)
// 	if err != nil {
// 		return nil, err
// 	}

// 	// Create builder with Docker command
// 	b := builderFactory(dockerCmd)

// 	buildConfig := &builder.BuildConfig{
// 		Source:     src,
// 		ContextDir: srcDir,
// 		Tag:        tag,
// 	}

// 	if _, err := b.Build(ctx, planResult.Plan, buildConfig); err != nil {
// 		return nil, err
// 	}

// 	return planResult.Plan, nil
// }

// BuildModel is a convenience helper that performs the full pipeline:
//  1. Source inspection of srcDir into a SourceInfo
//  2. Plan generation (GeneratePlan)
//  3. Plan execution via the supplied Builder
//
// It returns the Plan so callers can inspect or snapshot it.
func BuildModel(ctx context.Context, provider command.Command, src *project.SourceInfo) (*BuildModelResult, error) {
	// stack, err := stacks.SelectStack(ctx, src)
	// if err != nil {
	// 	return nil, fmt.Errorf("stack selection failed: %w", err)
	// }

	// b := builder.NewBuildKitBuilder(provider)

	// if err := stack.Plan(ctx, src, p); err != nil {
	// 	return nil, fmt.Errorf("stack %s planning failed: %w", stack.Name(), err)
	// }

	// if b == nil {
	// 	return nil, fmt.Errorf("builder cannot be nil")
	// }

	// // Initialize a basic config with Build field
	// cfg := &config.Config{
	// 	Build: &config.Build{},
	// }

	// src, err := project.NewSourceInfo(srcDir, cfg)
	// if err != nil {
	// 	return nil, fmt.Errorf("new source info: %w", err)
	// }
	// defer src.Close()

	// planResult, err := GeneratePlan(ctx, src)
	// if err != nil {
	// 	return nil, err
	// }

	tag := config.DockerImageName(src.RootPath())

	planResult, err := GeneratePlan(ctx, src)
	if err != nil {
		return nil, err
	}

	util.JSONPrettyPrint(planResult)

	buildConfig := &builder.BuildConfig{
		Source:     src,
		ContextDir: src.RootPath(),
		Tag:        tag,
	}

	fmt.Println("Running build plan")
	imgTag, imgCfg, err := RunBuildPlan(ctx, provider, planResult.Plan, buildConfig)
	if err != nil {
		return nil, err
	}

	fmt.Println("Build plan complete")

	return &BuildModelResult{
		ImageTag:    imgTag,
		ImageConfig: imgCfg,
	}, nil
}

type BuildModelResult struct {
	ImageTag    string         `json:"image_id"`
	ImageConfig *ocispec.Image `json:"image_config"`
}
