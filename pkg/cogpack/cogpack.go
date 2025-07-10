package cogpack

import (
	"context"
	"fmt"

	"github.com/replicate/cog/pkg/cogpack/core"
)

// GeneratePlan generates a complete build plan for the given project.
// This is the main entry point for the cogpack system.
func GeneratePlan(ctx context.Context, src *core.SourceInfo) (*PlanResult, error) {
	// 1. Initialize plan with platform
	plan := &Plan{
		Platform: Platform{
			OS:   "linux",
			Arch: "amd64",
		},
		Dependencies: make(map[string]Dependency),
		BuildPhases:  []Phase{},
		ExportPhases: []Phase{},
	}

	// 2. Select stack (first match wins)
	stack, err := SelectStack(ctx, src)
	if err != nil {
		return nil, fmt.Errorf("stack selection failed: %w", err)
	}

	// 3. Let stack orchestrate the build
	if err := stack.Plan(ctx, src, plan); err != nil {
		return nil, fmt.Errorf("stack %s planning failed: %w", stack.Name(), err)
	}

	// 4. Validate plan
	if err := ValidatePlan(plan); err != nil {
		return nil, fmt.Errorf("plan validation failed: %w", err)
	}

	// 5. Create result with metadata
	metadata := &PlanMetadata{
		Stack:     stack.Name(),
		BaseImage: plan.BaseImage.Build,
		Version:   "1.0",
	}

	return &PlanResult{
		Plan:     plan,
		Metadata: metadata,
		Timing:   map[string]string{}, // TODO: Add timing information
	}, nil
}

// ValidatePlan performs basic validation on a generated plan.
// This ensures the plan is well-formed and can be executed.
func ValidatePlan(plan *Plan) error {
	// Check that we have a base image
	if plan.BaseImage.Build == "" {
		return fmt.Errorf("no build base image specified")
	}
	if plan.BaseImage.Runtime == "" {
		return fmt.Errorf("no runtime base image specified")
	}

	// Validate stage ID uniqueness across all phases
	seenIDs := make(map[string]bool)

	// Check build phases
	for _, phase := range plan.BuildPhases {
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
	for _, phase := range plan.ExportPhases {
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
	for _, phase := range plan.BuildPhases {
		for _, stage := range phase.Stages {
			if err := validateStageInput(plan, stage); err != nil {
				return fmt.Errorf("stage %q input validation failed: %w", stage.ID, err)
			}
		}
	}

	for _, phase := range plan.ExportPhases {
		for _, stage := range phase.Stages {
			if err := validateStageInput(plan, stage); err != nil {
				return fmt.Errorf("stage %q input validation failed: %w", stage.ID, err)
			}
		}
	}

	return nil
}

// validateStageInput ensures a stage's input can be resolved
func validateStageInput(plan *Plan, stage Stage) error {
	input := stage.Source

	// Check if input refers to an image
	if input.Image != "" {
		// Image inputs are always valid (we assume they exist)
		return nil
	}

	// Check if input refers to another stage
	if input.Stage != "" {
		if plan.GetStage(input.Stage) == nil {
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
