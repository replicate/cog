package providers

import (
	"context"

	"github.com/replicate/cog/pkg/base_images"
	"github.com/replicate/cog/pkg/cogpack/core"
)

type BaseImageProvider struct{}

func (p *BaseImageProvider) Name() string {
	return "base-image"
}

func (p *BaseImageProvider) Configure(ctx context.Context, src *core.SourceInfo) error { return nil }

func (p *BaseImageProvider) Detect(ctx context.Context, src *core.SourceInfo) (bool, error) {
	return true, nil
}

func (p *BaseImageProvider) Plan(ctx context.Context, src *core.SourceInfo, plan *core.Plan) error {
	// var constraints []base_images.Constraint

	// for _, dep := range plan.Dependencies {
	// 	if dep.Name == "python" {
	// 		constraints = append(constraints, base_images.PythonConstraint(dep.ResolvedVersion))
	// 	}
	// }
	// baseImage, err := base_images.ResolveBaseImage(constraints...)
	// if err != nil {
	// 	return err
	// }

	baseImage, err := base_images.ResolveBaseImage(base_images.PythonConstraint("~>3.13"), base_images.ForAccelerator(base_images.AcceleratorCPU))
	if err != nil {
		return err
	}

	plan.BuildSteps = append(plan.BuildSteps, core.Stage{
		Name: "base-image",
		Inputs: []core.Input{
			{
				Image: baseImage.DevTag,
			},
		},
	})

	plan.ExportSteps = append(plan.ExportSteps, core.Stage{
		Name: "base-image",
		Inputs: []core.Input{
			{
				Image: baseImage.RunTag,
			},
		},
	})
	return nil
}
