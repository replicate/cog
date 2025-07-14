package plan

import "fmt"

// ValidatePlan performs comprehensive validation on a generated plan.
// This ensures the plan is well-formed and can be executed.
func ValidatePlan(p *Plan) error {
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

	// Validate context references - ensure all referenced contexts exist
	if err := validateContextReferences(p); err != nil {
		return fmt.Errorf("context validation failed: %w", err)
	}

	return nil
}

// validateStageInput ensures a stage's input can be resolved
func validateStageInput(p *Plan, stage *Stage) error {
	input := stage.Source

	// Check if input refers to a phase
	if input.Phase != "" {
		// Validate the phase exists and has stages
		phaseResult := p.GetPhaseResult(input.Phase)
		if phaseResult.Stage == "" {
			return fmt.Errorf("phase %q has no stages or does not exist", input.Phase)
		}
		// Note: We can't validate that the referenced stage exists yet since it might be
		// defined later in the plan. This will be caught during translation.
		return nil
	}

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

// validateContextReferences ensures all referenced contexts exist in the plan
func validateContextReferences(p *Plan) error {
	contextRefs := make(map[string]bool)
	
	// Collect all context references from operations
	for _, phase := range append(p.BuildPhases, p.ExportPhases...) {
		for _, stage := range phase.Stages {
			for _, op := range stage.Operations {
				switch o := op.(type) {
				case Exec:
					for _, mount := range o.Mounts {
						if mount.Source.Local != "" {
							contextRefs[mount.Source.Local] = true
						}
					}
				case Copy:
					if o.From.Local != "" {
						contextRefs[o.From.Local] = true
					}
				case Add:
					if o.From.Local != "" {
						contextRefs[o.From.Local] = true
					}
				}
			}
		}
	}
	
	// Validate all referenced contexts exist
	for contextName := range contextRefs {
		if _, exists := p.Contexts[contextName]; !exists {
			return fmt.Errorf("context %q referenced but not defined", contextName)
		}
	}
	
	return nil
}
