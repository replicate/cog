package python

import (
	"context"

	"github.com/replicate/cog/pkg/cogpack/plan"
	"github.com/replicate/cog/pkg/cogpack/project"
	"github.com/replicate/cog/pkg/dockerfile"
)

type CogWheelBlock struct{}

func (b *CogWheelBlock) Name() string {
	return "cog-wheel"
}

func (b *CogWheelBlock) Detect(ctx context.Context, src *project.SourceInfo) (bool, error) {
	return true, nil
}

func (b *CogWheelBlock) Dependencies(ctx context.Context, src *project.SourceInfo) ([]*plan.Dependency, error) {
	return nil, nil
}

func (b *CogWheelBlock) Plan(ctx context.Context, src *project.SourceInfo, p *plan.Plan) error {
	// Initialize contexts map if it doesn't exist
	if p.Contexts == nil {
		p.Contexts = make(map[string]*plan.BuildContext)
	}

	// Add wheel context to plan
	p.Contexts["wheel-context"] = &plan.BuildContext{
		Name:        "wheel-context",
		SourceBlock: "cog-wheel",
		Description: "Cog wheel file for installation",
		Metadata: map[string]string{
			"type": "embedded-wheel",
		},
		FS: dockerfile.CogEmbed,
	}

	stage, err := p.AddStage(plan.PhaseAppDeps, "cog-wheel", "cog-wheel")
	if err != nil {
		return err
	}

	// Install the cog wheel file and pydantic dependency using mount
	stage.Operations = append(stage.Operations, plan.Exec{
		Command: "/uv/uv pip install --python /venv/bin/python /mnt/wheel/embed/*.whl 'pydantic>=1.9,<3'",
		Mounts: []plan.Mount{
			{
				Source: plan.Input{Local: "wheel-context"},
				Target: "/mnt/wheel",
			},
		},
	})

	return nil
}
