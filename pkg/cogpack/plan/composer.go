package plan

import (
	"errors"
	"fmt"
	"iter"
	"slices"
	"strings"

	"github.com/replicate/cog/pkg/cogpack/baseimg"
	"github.com/replicate/cog/pkg/util/iterext"
)

// PlanComposer provides an API for composing plans during the build process.
// It handles plan-time concerns like phases and automatic input resolution,
// then converts to a normalized Plan structure for execution.
type PlanComposer struct {
	platform     Platform
	dependencies map[string]*Dependency
	baseImage    *baseimg.BaseImage
	contexts     map[string]*BuildContext
	exportConfig *ExportConfig

	// Phases in insertion order
	buildPhases  []*ComposerPhase
	exportPhases []*ComposerPhase

	// All stages by ID for lookups
	// stages map[string]*ComposerStage
}

// PhaseKey represents the different phases of the build process
type PhaseKey string

const (
	// Build phases
	PhaseBase          PhaseKey = "build.00-base"           // base image setup
	PhaseSystemDeps    PhaseKey = "build.01-system-deps"    // apt packages, system tools
	PhaseRuntime       PhaseKey = "build.02-runtime"        // language runtime (python, node)
	PhaseFrameworkDeps PhaseKey = "build.03-framework-deps" // torch, tensorflow, heavy deps
	PhaseAppDeps       PhaseKey = "build.04-app-deps"       // requirements.txt, package.json
	PhaseAppBuild      PhaseKey = "build.05-app-build"      // compile, build artifacts
	PhaseAppSource     PhaseKey = "build.06-app-source"     // copy source code

	// Export phases (for runtime image)
	ExportPhaseBase    PhaseKey = "export.00-base"    // runtime base setup
	ExportPhaseRuntime PhaseKey = "export.01-runtime" // copy runtime deps
	ExportPhaseApp     PhaseKey = "export.02-app"     // copy app artifacts
	ExportPhaseConfig  PhaseKey = "export.03-config"  // final config, entrypoint
)

// IsExportPhase returns true if this phase is part of the export process
func (p PhaseKey) IsExportPhase() bool {
	return strings.HasPrefix(string(p), "export.")
}

func (p PhaseKey) IsBuildPhase() bool {
	return strings.HasPrefix(string(p), "build.")
}

// ComposerPhase represents a phase during composition
type ComposerPhase struct {
	Name   PhaseKey
	Stages []*ComposerStage

	composer *PlanComposer
}

func (p *ComposerPhase) appendStage(s *ComposerStage) {
	s.phase = p
	s.composer = p.composer
	p.Stages = append(p.Stages, s)
}

func (p *ComposerPhase) lastStage() *ComposerStage {
	if len(p.Stages) == 0 {
		return nil
	}
	return p.Stages[len(p.Stages)-1]
}

func (p *ComposerPhase) previousStage(stage *ComposerStage) *ComposerStage {
	stageIdx := slices.Index(p.Stages, stage)
	if stageIdx > 0 {
		return p.Stages[stageIdx-1]
	}
	return nil
}

// NewPlanComposer creates a new plan composer
func NewPlanComposer() *PlanComposer {
	return &PlanComposer{
		platform: Platform{
			OS:   "linux",
			Arch: "amd64",
		},
		dependencies: make(map[string]*Dependency),
		contexts:     make(map[string]*BuildContext),
		// stages:       make(map[string]*ComposerStage),
	}
}

// SetPlatform sets the target platform
func (pc *PlanComposer) SetPlatform(platform Platform) {
	pc.platform = platform
}

// SetDependencies sets the resolved dependencies
func (pc *PlanComposer) SetDependencies(deps map[string]*Dependency) {
	pc.dependencies = deps
}

// SetBaseImage sets the base image configuration
func (pc *PlanComposer) SetBaseImage(baseImage *baseimg.BaseImage) {
	pc.baseImage = baseImage
}

// SetExportConfig sets the final export configuration
func (pc *PlanComposer) SetExportConfig(export *ExportConfig) {
	pc.exportConfig = export
}

// AddContext adds a build context
func (pc *PlanComposer) AddContext(name string, context *BuildContext) {
	if pc.contexts == nil {
		pc.contexts = make(map[string]*BuildContext)
	}
	pc.contexts[name] = context
}

// GetDependency returns a specific dependency by name
func (pc *PlanComposer) GetDependency(name string) (*Dependency, bool) {
	dep, exists := pc.dependencies[name]
	return dep, exists
}

// GetBaseImage returns the base image
func (pc *PlanComposer) GetBaseImage() *baseimg.BaseImage {
	return pc.baseImage
}

var ErrDuplicateStageID = errors.New("stage ID already exists")

func (pc *PlanComposer) BuildStages() iter.Seq[*ComposerStage] {
	return func(yield func(*ComposerStage) bool) {
		for _, phase := range pc.buildPhases {
			for _, stage := range phase.Stages {
				if !yield(stage) {
					return
				}
			}
		}
	}
}

func (pc *PlanComposer) ExportStages() iter.Seq[*ComposerStage] {
	return func(yield func(*ComposerStage) bool) {
		for _, phase := range pc.exportPhases {
			for _, stage := range phase.Stages {
				if !yield(stage) {
					return
				}
			}
		}
	}
}

func (pc *PlanComposer) AllStages() iter.Seq[*ComposerStage] {
	return iterext.Concat(pc.BuildStages(), pc.ExportStages())
}

type StageOpt func(*ComposerStage)

func WithSource(source SourceOpt) StageOpt {
	return func(stage *ComposerStage) {
		stage.Source = source()
	}
}

func WithName(name string) StageOpt {
	return func(stage *ComposerStage) {
		stage.Name = name
	}
}

// AddStage adds a new stage to the specified phase
func (pc *PlanComposer) AddStage(phaseName PhaseKey, stageID string, opts ...StageOpt) (*ComposerStage, error) {
	// Check for duplicate stage ID
	for stage := range pc.AllStages() {
		if stage.ID == stageID {
			return nil, ErrDuplicateStageID
		}
	}

	// Get or create the phase
	phase := pc.getOrCreatePhase(phaseName)

	// Create the stage with default auto input
	stage := &ComposerStage{
		ID:     stageID,
		Source: Input{Auto: true},
	}
	for _, opt := range opts {
		opt(stage)
	}

	phase.appendStage(stage)

	return stage, nil
}

// GetStage retrieves a stage by ID
func (pc *PlanComposer) GetStage(stageID string) *ComposerStage {
	for stage := range pc.AllStages() {
		if stage.ID == stageID {
			return stage
		}
	}
	return nil
}

// HasProvider checks if a package is available from base image or build stages
func (pc *PlanComposer) HasProvider(packageName string) bool {
	// Check base image first
	if pc.baseImage != nil {
		if _, exists := pc.baseImage.Metadata.Packages[packageName]; exists {
			return true
		}
	}

	// Check all stages
	for stage := range pc.AllStages() {
		for _, provided := range stage.Provides {
			if provided == packageName {
				return true
			}
		}
	}

	return false
}

// Compose converts the PlanComposer to a normalized Plan
func (pc *PlanComposer) Compose() (*Plan, error) {
	// Create the base plan
	plan := &Plan{
		Platform:     pc.platform,
		Export:       pc.exportConfig,
		Contexts:     pc.contexts,
		BuildStages:  []*Stage{},
		ExportStages: []*Stage{},
	}

	for cs := range pc.BuildStages() {
		composedStage, err := pc.convertStage(cs)
		if err != nil {
			return nil, fmt.Errorf("converting stage %q: %w", cs.ID, err)
		}
		plan.BuildStages = append(plan.BuildStages, composedStage)
	}

	for cs := range pc.ExportStages() {
		outputStage, err := pc.convertStage(cs)
		if err != nil {
			return nil, fmt.Errorf("converting stage %q: %w", cs.ID, err)
		}
		plan.ExportStages = append(plan.ExportStages, outputStage)
	}

	// // Validate the final plan
	// if err := plan.Validate(); err != nil {
	// 	return nil, fmt.Errorf("plan validation failed: %w", err)
	// }

	return plan, nil
}

// convertStage converts a ComposerStage to a Stage with resolved inputs
func (pc *PlanComposer) convertStage(cs *ComposerStage) (*Stage, error) {
	stage := &Stage{
		ID:         cs.ID,
		Name:       cs.Name,
		Operations: cs.Operations,
		Env:        cs.Env,
		Dir:        cs.Dir,
		Provides:   cs.Provides,
	}

	// Resolve the input
	resolvedInput, err := pc.resolveInput(cs)
	if err != nil {
		return nil, fmt.Errorf("resolving input for stage %q: %w", cs.ID, err)
	}
	stage.Source = resolvedInput

	return stage, nil
}

// resolveInput resolves any input type to a concrete input
func (pc *PlanComposer) resolveInput(stage *ComposerStage) (Input, error) {
	input := stage.Source

	// Handle Auto resolution
	if input.Auto {
		if stage := stage.inputStage(); stage != nil {
			return Input{Stage: stage.ID}, nil
		}
		return Input{}, fmt.Errorf("cannot resolve auto input for stage %q: no previous stage", stage.ID)
	}

	if input.Stage != "" {
		if stage := pc.GetStage(input.Stage); stage != nil {
			return Input{Stage: stage.ID}, nil
		}
		return Input{}, fmt.Errorf("stage %q does not exist", input.Stage)
	}

	if input.Phase != "" {
		composerPhase := pc.findComposerPhase(input.Phase)
		if composerPhase == nil || len(composerPhase.Stages) == 0 {
			return Input{}, fmt.Errorf("phase %q has no stages or does not exist", input.Phase)
		}
		stage := composerPhase.lastStage()
		if stage != nil {
			return Input{Stage: stage.ID}, nil
		}
		return Input{}, fmt.Errorf("phase %q has no stages", input.Phase)
	}

	// All other input types (Image, Stage, Local, URL, Scratch) are already concrete
	return input, nil
}

func (pc *PlanComposer) previousPhase(phase *ComposerPhase) *ComposerPhase {
	if phase.Name.IsBuildPhase() {
		if idx := slices.Index(pc.buildPhases, phase); idx > 0 {
			return pc.buildPhases[idx-1]
		}
	} else if phase.Name.IsExportPhase() {
		if idx := slices.Index(pc.exportPhases, phase); idx > 0 {
			return pc.exportPhases[idx-1]
		}
	}
	return nil
}

func (pc *PlanComposer) previousStage(stage *ComposerStage) *ComposerStage {
	phase := stage.phase
	if prevStage := phase.previousStage(stage); prevStage != nil {
		return prevStage
	}

	for {
		phase = pc.previousPhase(phase)
		if phase == nil {
			return nil
		}
		if prevStage := phase.lastStage(); prevStage != nil {
			return prevStage
		}
	}
}

// resolveAutoInput resolves an Auto input based on context
func (pc *PlanComposer) resolveAutoInput(currentPhase PhaseKey, stageIndex int) (Input, error) {
	// If not the first stage in phase, use previous stage
	if stageIndex > 0 {
		// Look in the current phase being built
		currentComposerPhase := pc.findComposerPhase(currentPhase)
		if currentComposerPhase != nil && stageIndex-1 < len(currentComposerPhase.Stages) {
			prevStage := currentComposerPhase.Stages[stageIndex-1]
			return Input{Stage: prevStage.ID}, nil
		}
	}

	// First stage in phase - find previous phase with stages
	prevPhase := pc.findPreviousComposerPhaseWithStages(currentPhase)
	if prevPhase != nil && len(prevPhase.Stages) > 0 {
		lastStage := prevPhase.Stages[len(prevPhase.Stages)-1]
		return Input{Stage: lastStage.ID}, nil
	}

	// No previous phase with stages - use base image
	if pc.baseImage != nil {
		if currentPhase.IsExportPhase() {
			return Input{Image: pc.baseImage.Runtime}, nil
		}
		return Input{Image: pc.baseImage.Build}, nil
	}

	return Input{}, fmt.Errorf("cannot resolve auto input: no previous stage, phase, or base image")
}

// getOrCreatePhase finds or creates a phase
func (pc *PlanComposer) getOrCreatePhase(phaseName PhaseKey) *ComposerPhase {
	// Check existing phases
	phases := &pc.buildPhases
	if phaseName.IsExportPhase() {
		phases = &pc.exportPhases
	}

	for _, phase := range *phases {
		if phase.Name == phaseName {
			return phase
		}
	}

	// Create new phase
	phase := &ComposerPhase{
		Name:     phaseName,
		Stages:   []*ComposerStage{},
		composer: pc,
	}

	*phases = append(*phases, phase)
	return phase
}

// findComposerPhase finds a composer phase by name
func (pc *PlanComposer) findComposerPhase(phaseName PhaseKey) *ComposerPhase {
	phases := pc.buildPhases
	if phaseName.IsExportPhase() {
		phases = pc.exportPhases
	}

	for _, phase := range phases {
		if phase.Name == phaseName {
			return phase
		}
	}
	return nil
}

// findPreviousComposerPhaseWithStages finds the previous composer phase that has stages
func (pc *PlanComposer) findPreviousComposerPhaseWithStages(currentPhase PhaseKey) *ComposerPhase {
	phases := pc.buildPhases
	if currentPhase.IsExportPhase() {
		phases = pc.exportPhases
	}

	var prevPhase *ComposerPhase
	for _, phase := range phases {
		if phase.Name == currentPhase {
			return prevPhase
		}
		if len(phase.Stages) > 0 {
			prevPhase = phase
		}
	}
	return nil
}

// Convenience methods for ComposerStage

// ComposerStage represents a stage during composition
type ComposerStage struct {
	ID         string
	Name       string
	Source     Input // can have Phase ref, Auto, etc.
	Operations []Op
	Env        []string
	Dir        string
	Provides   []string

	// Bidirectional references for API convenience
	phase    *ComposerPhase
	composer *PlanComposer
}

func (cs *ComposerStage) inputStage() *ComposerStage {
	return cs.composer.previousStage(cs)
}

// AddOperation adds an operation to the stage
func (cs *ComposerStage) AddOperation(op Op) *ComposerStage {
	cs.Operations = append(cs.Operations, op)
	return cs
}

// AddOperations adds multiple operations to the stage
func (cs *ComposerStage) AddOperations(ops ...Op) *ComposerStage {
	cs.Operations = append(cs.Operations, ops...)
	return cs
}

// SetEnv sets environment variables for the stage
func (cs *ComposerStage) SetEnv(env []string) *ComposerStage {
	cs.Env = env
	return cs
}

// SetDir sets the working directory for the stage
func (cs *ComposerStage) SetDir(dir string) *ComposerStage {
	cs.Dir = dir
	return cs
}

// SetProvides sets what this stage provides
func (cs *ComposerStage) SetProvides(provides ...string) *ComposerStage {
	cs.Provides = provides
	return cs
}

// GetPhase returns the phase this stage belongs to
func (cs *ComposerStage) GetPhase() *ComposerPhase {
	return cs.phase
}

// GetComposer returns the plan composer
func (cs *ComposerStage) GetComposer() *PlanComposer {
	return cs.composer
}
