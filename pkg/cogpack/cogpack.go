package cogpack

import (
	"context"
	"fmt"

	"github.com/replicate/cog/pkg/cogpack/plan"
	"github.com/replicate/cog/pkg/cogpack/project"
	"github.com/replicate/cog/pkg/cogpack/stacks"
)

// GeneratePlan generates a complete build plan for the given project.
// This is the main entry point for the cogpack system.
func GeneratePlan(ctx context.Context, src *project.SourceInfo) (*plan.PlanResult, error) {
	// 1. Initialize plan with platform
	p := &plan.Plan{
		Platform: plan.Platform{
			OS:   "linux",
			Arch: "amd64",
		},
		Dependencies: make(map[string]plan.Dependency),
		BuildPhases:  []plan.Phase{},
		ExportPhases: []plan.Phase{},
	}

	// 2. Select stack (first match wins)
	stack, err := stacks.SelectStack(ctx, src)
	if err != nil {
		return nil, fmt.Errorf("stack selection failed: %w", err)
	}

	// 3. Let stack orchestrate the build
	if err := stack.Plan(ctx, src, p); err != nil {
		return nil, fmt.Errorf("stack %s planning failed: %w", stack.Name(), err)
	}

	// 4. Validate plan
	if err := ValidatePlan(p); err != nil {
		return nil, fmt.Errorf("plan validation failed: %w", err)
	}

	// 5. Create result with metadata
	metadata := &plan.PlanMetadata{
		Stack:     stack.Name(),
		BaseImage: p.BaseImage.Build,
		Version:   "1.0",
	}

	return &plan.PlanResult{
		Plan:     p,
		Metadata: metadata,
		Timing:   map[string]string{}, // TODO: Add timing information
	}, nil
}

// ValidatePlan performs basic validation on a generated plan.
// This ensures the plan is well-formed and can be executed.
func ValidatePlan(p *plan.Plan) error {
	// Check that we have a base image
	if p.BaseImage.Build == "" {
		return fmt.Errorf("no build base image specified")
	}
	if p.BaseImage.Runtime == "" {
		return fmt.Errorf("no runtime base image specified")
	}

	// Validate stage ID uniqueness across all phases
	seenIDs := make(map[string]bool)

	// Check build phases
	for _, phase := range p.BuildPhases {
		for _, stage := range phase.Stages {
			if stage.ID == "" {
				return fmt.Errorf("stage %q has empty ID", stage.Name)
			}
			if seenIDs[stage.ID] {
				return fmt.Errorf("duplicate stage ID: %q", stage.ID)
			}
			seenIDs[stage.ID] = true
		}
	}

	// Check export phases
	for _, phase := range p.ExportPhases {
		for _, stage := range phase.Stages {
			if stage.ID == "" {
				return fmt.Errorf("stage %q has empty ID", stage.Name)
			}
			if seenIDs[stage.ID] {
				return fmt.Errorf("duplicate stage ID: %q", stage.ID)
			}
			seenIDs[stage.ID] = true
		}
	}

	// Validate stage inputs can be resolved
	for _, phase := range p.BuildPhases {
		for _, stage := range phase.Stages {
			if err := validateStageInput(p, stage); err != nil {
				return fmt.Errorf("stage %q input validation failed: %w", stage.ID, err)
			}
		}
	}

	for _, phase := range p.ExportPhases {
		for _, stage := range phase.Stages {
			if err := validateStageInput(p, stage); err != nil {
				return fmt.Errorf("stage %q input validation failed: %w", stage.ID, err)
			}
		}
	}

	return nil
}

// validateStageInput ensures a stage's input can be resolved
func validateStageInput(p *plan.Plan, stage plan.Stage) error {
	input := stage.Source

	// Check if input refers to an image
	if input.Image != "" {
		// Image inputs are always valid (we assume they exist)
		return nil
	}

	// Check if input refers to another stage
	if input.Stage != "" {
		if p.GetStage(input.Stage) == nil {
			return fmt.Errorf("stage input %q not found", input.Stage)
		}
		return nil
	}

	// Check if input refers to local context
	if input.Local != "" {
		// Local inputs are always valid (build context)
		return nil
	}

	// Stage has no input - this might be valid for some cases
	return nil
}
