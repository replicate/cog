package plan

import (
	"fmt"
	"iter"
	"slices"
	"strings"

	"github.com/replicate/cog/pkg/cogpack/baseimg"
)

// Composer provides an API for composing plans during the build process.
// It handles plan-time concerns like phases and automatic input resolution,
// then converts to a normalized Plan structure for execution.
type Composer struct {
	platform     Platform
	dependencies map[string]*Dependency
	baseImage    *baseimg.BaseImage
	contexts     map[string]*BuildContext
	exportConfig *ExportConfig
	phases       []*ComposerPhase // Single ordered list of all phases
}

// PhaseKey represents the different phases of the build process
type PhaseKey string

// PhaseType represents the type of phase (build or export)
type PhaseType int

const (
	PhaseTypeUnknown PhaseType = iota
	PhaseTypeBuild
	PhaseTypeExport
)

const (
	// Build phases
	PhaseBase          PhaseKey = "build.00-base"           // base image setup
	PhaseSystemDeps    PhaseKey = "build.01-system-deps"    // apt packages, system tools
	PhaseRuntime       PhaseKey = "build.02-runtime"        // language runtime (python, node)
	PhaseFrameworkDeps PhaseKey = "build.03-framework-deps" // torch, tensorflow, heavy deps
	PhaseAppDeps       PhaseKey = "build.04-app-deps"       // requirements.txt, package.json
	PhaseAppBuild      PhaseKey = "build.05-app-build"      // compile, build artifacts
	PhaseAppSource     PhaseKey = "build.06-app-source"     // copy source code
	PhaseBuildComplete PhaseKey = "build.99-complete"       // final build output

	// Export phases (for runtime image)
	ExportPhaseBase    PhaseKey = "export.00-base"    // runtime base setup
	ExportPhaseRuntime PhaseKey = "export.01-runtime" // copy runtime deps
	ExportPhaseApp     PhaseKey = "export.02-app"     // copy app artifacts
	ExportPhaseConfig  PhaseKey = "export.03-config"  // final config, entrypoint
)

// Type returns the type of phase (build or export)
func (p PhaseKey) Type() PhaseType {
	if strings.HasPrefix(string(p), "build.") {
		return PhaseTypeBuild
	}
	if strings.HasPrefix(string(p), "export.") {
		return PhaseTypeExport
	}
	return PhaseTypeUnknown
}

// ComposerPhase represents a phase during composition
type ComposerPhase struct {
	Key    PhaseKey
	Stages []*ComposerStage

	composer *Composer
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

// DefaultPhases returns the standard phases in order
func DefaultPhases() []PhaseKey {
	return []PhaseKey{
		// Build phases
		PhaseBase,
		PhaseSystemDeps,
		PhaseRuntime,
		PhaseFrameworkDeps,
		PhaseAppDeps,
		PhaseAppBuild,
		PhaseAppSource,
		PhaseBuildComplete,

		// Export phases
		ExportPhaseBase,
		ExportPhaseRuntime,
		ExportPhaseApp,
		ExportPhaseConfig,
	}
}

// newPlanComposerWithPhases creates a new plan composer with specific phases pre-registered
func newPlanComposerWithPhases(phases []PhaseKey) *Composer {
	c := &Composer{
		platform: Platform{
			OS:   "linux",
			Arch: "amd64",
		},
		dependencies: make(map[string]*Dependency),
		contexts:     make(map[string]*BuildContext),
		phases:       make([]*ComposerPhase, 0, len(phases)),
	}

	// Pre-register all phases in order
	for _, phase := range phases {
		c.getOrCreatePhase(phase)
	}

	return c
}

// NewPlanComposer creates a new plan composer with default phases pre-registered
func NewPlanComposer() *Composer {
	return newPlanComposerWithPhases(DefaultPhases())
}

func (pc *Composer) Debug() map[string]any {
	return map[string]any{
		"platform":     pc.platform,
		"dependencies": pc.dependencies,
		"contexts":     pc.contexts,
		"baseImage":    pc.baseImage,
		"exportConfig": pc.exportConfig,
		"phases":       pc.phases,
	}
}

// SetPlatform sets the target platform
func (pc *Composer) SetPlatform(platform Platform) {
	pc.platform = platform
}

// SetDependencies sets the resolved dependencies
func (pc *Composer) SetDependencies(deps map[string]*Dependency) {
	pc.dependencies = deps
}

// SetBaseImage sets the base image configuration
func (pc *Composer) SetBaseImage(baseImage *baseimg.BaseImage) {
	pc.baseImage = baseImage
}

// SetExportConfig sets the final export configuration
func (pc *Composer) SetExportConfig(export *ExportConfig) {
	pc.exportConfig = export
}

// AddContext adds a build context
func (pc *Composer) AddContext(name string, context *BuildContext) {
	if pc.contexts == nil {
		pc.contexts = make(map[string]*BuildContext)
	}
	pc.contexts[name] = context
}

// GetDependency returns a specific dependency by name
func (pc *Composer) GetDependency(name string) (*Dependency, bool) {
	dep, exists := pc.dependencies[name]
	return dep, exists
}

// GetBaseImage returns the base image
func (pc *Composer) GetBaseImage() *baseimg.BaseImage {
	return pc.baseImage
}

// AllStages returns an iterator over all stages in phase order
func (pc *Composer) AllStages() iter.Seq[*ComposerStage] {
	return func(yield func(*ComposerStage) bool) {
		for _, phase := range pc.phases {
			for _, stage := range phase.Stages {
				if !yield(stage) {
					return
				}
			}
		}
	}
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
func (pc *Composer) AddStage(phaseName PhaseKey, stageID string, opts ...StageOpt) (*ComposerStage, error) {
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
func (pc *Composer) GetStage(stageID string) *ComposerStage {
	for stage := range pc.AllStages() {
		if stage.ID == stageID {
			return stage
		}
	}
	return nil
}

// HasProvider checks if a package is available from base image or build stages
func (pc *Composer) HasProvider(packageName string) bool {
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
func (pc *Composer) Compose() (*Plan, error) {
	// Create the base plan
	plan := &Plan{
		Platform: pc.platform,
		Export:   pc.exportConfig,
		Contexts: pc.contexts,
		Stages:   []*Stage{},
	}

	// Convert all stages in order
	for cs := range pc.AllStages() {
		composedStage, err := pc.convertStage(cs)
		if err != nil {
			return nil, fmt.Errorf("converting stage %q: %w", cs.ID, err)
		}
		plan.Stages = append(plan.Stages, composedStage)
	}

	// // Validate the final plan
	// if err := plan.Validate(); err != nil {
	// 	return nil, fmt.Errorf("plan validation failed: %w", err)
	// }

	return plan, nil
}

// convertStage converts a ComposerStage to a Stage with resolved inputs
func (pc *Composer) convertStage(cs *ComposerStage) (*Stage, error) {
	// Resolve the stage input
	resolvedInput, err := pc.resolveInputFromStage(cs.Source, cs)
	if err != nil {
		return nil, fmt.Errorf("resolving input for stage %q: %w", cs.ID, err)
	}

	// Resolve operation inputs
	resolvedOperations, err := pc.resolveOperationInputs(cs.Operations, cs)
	if err != nil {
		return nil, fmt.Errorf("resolving operation inputs for stage %q: %w", cs.ID, err)
	}

	stage := &Stage{
		ID:         cs.ID,
		Name:       cs.Name,
		PhaseKey:   cs.phase.Key,
		Source:     *resolvedInput,
		Operations: resolvedOperations,
		Env:        cs.Env,
		Dir:        cs.Dir,
		Provides:   cs.Provides,
	}

	return stage, nil
}

// resolvePhaseInputStage returns the stage that would be provided as input TO the given phase
// (i.e., the last stage before this phase starts)
func (pc *Composer) resolvePhaseInputStage(phase *ComposerPhase) (*ComposerStage, error) {
	idx := slices.Index(pc.phases, phase)
	if idx == -1 {
		return nil, ErrPhaseNotFound
	}
	if idx == 0 {
		return nil, ErrNoInputStage
	}

	for i := idx - 1; i >= 0; i-- {
		previousPhase := pc.phases[i]
		if previousStage := previousPhase.lastStage(); previousStage != nil {
			return previousStage, nil
		}
	}

	return nil, ErrNoInputStage
}

// resolvePhaseOutputStage returns the stage that results FROM the given phase
// (i.e., the last stage of this phase, or if empty, walks backwards to find the last stage)
func (pc *Composer) resolvePhaseOutputStage(phase *ComposerPhase) (*ComposerStage, error) {
	if phase == nil || !pc.phaseExists(phase.Key) {
		return nil, ErrPhaseNotFound
	}

	if stage := phase.lastStage(); stage != nil {
		return stage, nil
	}
	return pc.resolvePhaseInputStage(phase)
}

// resolveStageInputStage returns the stage that would be provided as input TO the given stage
// (i.e., the last stage before this stage starts)
func (pc *Composer) resolveStageInputStage(stage *ComposerStage) (*ComposerStage, error) {
	var previousStage *ComposerStage
	for otherStage := range pc.AllStages() {
		if otherStage == stage {
			if previousStage == nil {
				return nil, ErrNoInputStage
			}
			return previousStage, nil
		}
		previousStage = otherStage
	}
	return nil, ErrStageNotFound
}

func (pc *Composer) resolveInputFromStage(input Input, stage *ComposerStage) (*Input, error) {
	if stage == nil || !pc.stageRegistered(stage) {
		return nil, ErrStageNotFound
	}

	// Handle Auto resolution
	if input.Auto {
		if inputStage, err := pc.resolveStageInputStage(stage); err == nil {
			return &Input{Stage: inputStage.ID}, nil
		} else {
			return nil, err
		}
	}

	if input.Stage != "" {
		if stage := pc.GetStage(input.Stage); stage != nil {
			return &Input{Stage: stage.ID}, nil
		}
		return nil, ErrStageNotFound
	}

	if input.Phase != "" {
		composerPhase := pc.getPhase(input.Phase)
		if composerPhase == nil {
			return nil, ErrPhaseNotFound
		}

		// Use resolvePhaseOutputStage to get the output from the phase
		if stage, err := pc.resolvePhaseOutputStage(composerPhase); err == nil {
			return &Input{Stage: stage.ID}, nil
		} else {
			return nil, err
		}
	}

	// All other input types (Image, Local, URL, Scratch) are already concrete
	return &input, nil
}

// resolveOperationInputs resolves Input fields within operations
func (pc *Composer) resolveOperationInputs(operations []Op, stage *ComposerStage) ([]Op, error) {
	resolved := make([]Op, len(operations))

	for i, op := range operations {
		switch typed := op.(type) {
		case Copy:
			resolvedInput, err := pc.resolveInputFromStage(typed.From, stage)
			if err != nil {
				return nil, fmt.Errorf("resolving Copy.From input: %w", err)
			}
			typed.From = *resolvedInput
			resolved[i] = typed

		case Add:
			if !typed.From.IsEmpty() {
				resolvedInput, err := pc.resolveInputFromStage(typed.From, stage)
				if err != nil {
					return nil, fmt.Errorf("resolving Add.From input: %w", err)
				}
				typed.From = *resolvedInput
			}
			resolved[i] = typed

		case Exec:
			// Resolve mount sources
			if len(typed.Mounts) > 0 {
				resolvedMounts := make([]Mount, len(typed.Mounts))
				for j, mount := range typed.Mounts {
					resolvedInput, err := pc.resolveInputFromStage(mount.Source, stage)
					if err != nil {
						return nil, fmt.Errorf("resolving Mount.Source input: %w", err)
					}
					resolvedMounts[j] = Mount{
						Source: *resolvedInput,
						Target: mount.Target,
					}
				}
				typed.Mounts = resolvedMounts
			}
			resolved[i] = typed

		default:
			// Operations without Input fields can be passed through unchanged
			resolved[i] = op
		}
	}

	return resolved, nil
}

// getOrCreatePhase finds or creates a phase
func (pc *Composer) getOrCreatePhase(phaseName PhaseKey) *ComposerPhase {
	// Check existing phases
	for _, phase := range pc.phases {
		if phase.Key == phaseName {
			return phase
		}
	}

	// Create new phase
	phase := &ComposerPhase{
		Key:      phaseName,
		Stages:   []*ComposerStage{},
		composer: pc,
	}

	pc.phases = append(pc.phases, phase)
	return phase
}

func (pc *Composer) stageRegistered(stage *ComposerStage) bool {
	for other := range pc.AllStages() {
		if other == stage {
			return true
		}
	}
	return false
}

func (pc *Composer) phaseExists(phaseKey PhaseKey) bool {
	return pc.getPhase(phaseKey) != nil
}

// getPhase finds a composer phase by name
func (pc *Composer) getPhase(phaseName PhaseKey) *ComposerPhase {
	for _, phase := range pc.phases {
		if phase.Key == phaseName {
			return phase
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
	composer *Composer
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
func (cs *ComposerStage) GetComposer() *Composer {
	return cs.composer
}
