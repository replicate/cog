package cogpack

import (
	"context"
	"fmt"

	"github.com/replicate/cog/pkg/cogpack/builder"
	"github.com/replicate/cog/pkg/cogpack/plan"
	"github.com/replicate/cog/pkg/cogpack/project"
	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker/command"
)

// ExecutePlan executes a pre-generated Plan using the supplied Builder.
func ExecutePlan(ctx context.Context, p *plan.Plan, buildContextDir, tag string, b builder.Builder) error {
	if b == nil {
		return fmt.Errorf("builder cannot be nil")
	}
	return b.Build(ctx, p, buildContextDir, tag)
}

// BuildWithDocker is a convenience helper that performs the full pipeline:
//  1. Source inspection of srcDir into a SourceInfo
//  2. Plan generation (GeneratePlan)
//  3. Plan execution via the supplied Builder that uses Docker
//
// It returns the Plan so callers can inspect or snapshot it.
func BuildWithDocker(ctx context.Context, srcDir, tag string, dockerCmd command.Command, builderFactory func(command.Command) builder.Builder) (*plan.Plan, error) {
	if dockerCmd == nil {
		return nil, fmt.Errorf("docker command cannot be nil")
	}
	if builderFactory == nil {
		return nil, fmt.Errorf("builder factory cannot be nil")
	}

	// Initialize a basic config with Build field
	cfg := &config.Config{
		Build: &config.Build{},
	}

	src, err := project.NewSourceInfo(srcDir, cfg)
	if err != nil {
		return nil, fmt.Errorf("new source info: %w", err)
	}
	defer src.Close()

	planResult, err := GeneratePlan(ctx, src)
	if err != nil {
		return nil, err
	}

	// Create builder with Docker command
	b := builderFactory(dockerCmd)

	if err := b.Build(ctx, planResult.Plan, srcDir, tag); err != nil {
		return nil, err
	}

	return planResult.Plan, nil
}

// Build is a convenience helper that performs the full pipeline:
//  1. Source inspection of srcDir into a SourceInfo
//  2. Plan generation (GeneratePlan)
//  3. Plan execution via the supplied Builder
//
// It returns the Plan so callers can inspect or snapshot it.
func Build(ctx context.Context, srcDir, tag string, b builder.Builder) (*plan.Plan, error) {
	if b == nil {
		return nil, fmt.Errorf("builder cannot be nil")
	}

	// Initialize a basic config with Build field
	cfg := &config.Config{
		Build: &config.Build{},
	}

	src, err := project.NewSourceInfo(srcDir, cfg)
	if err != nil {
		return nil, fmt.Errorf("new source info: %w", err)
	}
	defer src.Close()

	planResult, err := GeneratePlan(ctx, src)
	if err != nil {
		return nil, err
	}

	if err := b.Build(ctx, planResult.Plan, srcDir, tag); err != nil {
		return nil, err
	}

	return planResult.Plan, nil
}
