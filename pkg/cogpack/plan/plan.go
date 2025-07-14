package plan

import (
	"fmt"
	"io/fs"

	"github.com/replicate/cog/pkg/cogpack/baseimg"
)

// Package cogpack implements the Stack → Blocks → Plan → Builder architecture
// described in cogpack.claude.md. See "Core Components" section for component
// responsibilities and "Design Reasoning" for architectural decisions.

// Plan represents the complete build specification that can be executed by a builder
type Plan struct {
	Platform     Platform                 `json:"platform"`      // linux/amd64
	Dependencies map[string]*Dependency   `json:"dependencies"`  // resolved versions
	BaseImage    *baseimg.BaseImage       `json:"base_image"`    // build/runtime images
	BuildPhases  []*Phase                 `json:"build_phases"`  // organized build work
	ExportPhases []*Phase                 `json:"export_phases"` // runtime image assembly
	Export       *ExportConfig            `json:"export"`        // final image config
	Contexts     map[string]*BuildContext `json:"contexts"`      // build contexts for mounting
}

// Platform represents the target platform for the build
type Platform struct {
	OS   string `json:"os"`   // "linux"
	Arch string `json:"arch"` // "amd64"
}

// Dependency represents a resolved dependency constraint
type Dependency struct {
	Name             string `json:"name"`              // e.g., "python", "torch"
	Provider         string `json:"provider"`          // block that requested it
	RequestedVersion string `json:"requested_version"` // original constraint
	ResolvedVersion  string `json:"resolved_version"`  // final resolved version
	Source           string `json:"source"`            // where it came from
}

// Phase represents a logical group of stages in the build process
type Phase struct {
	Name   StagePhase `json:"name"`   // PhaseSystemDeps, PhaseFrameworkDeps, etc.
	Stages []*Stage   `json:"stages"` // all stages within this phase
}

// StagePhase represents the different phases of the build process
type StagePhase string

const (
	// Build phases
	PhaseBase          StagePhase = "base"           // base image setup
	PhaseSystemDeps    StagePhase = "system-deps"    // apt packages, system tools
	PhaseRuntime       StagePhase = "runtime"        // language runtime (python, node)
	PhaseFrameworkDeps StagePhase = "framework-deps" // torch, tensorflow, heavy deps
	PhaseAppDeps       StagePhase = "app-deps"       // requirements.txt, package.json
	PhaseAppBuild      StagePhase = "app-build"      // compile, build artifacts
	PhaseAppSource     StagePhase = "app-source"     // copy source code

	// Export phases (for runtime image)
	ExportPhaseBase    StagePhase = "export-base"    // runtime base setup
	ExportPhaseRuntime StagePhase = "export-runtime" // copy runtime deps
	ExportPhaseApp     StagePhase = "export-app"     // copy app artifacts
	ExportPhaseConfig  StagePhase = "export-config"  // final config, entrypoint
)

// IsExportPhase returns true if this phase is part of the export process
func (p StagePhase) IsExportPhase() bool {
	switch p {
	case ExportPhaseBase, ExportPhaseRuntime, ExportPhaseApp, ExportPhaseConfig:
		return true
	default:
		return false
	}
}

// Stage represents a build phase with explicit state management
type Stage struct {
	ID         string   `json:"id"`                          // unique identifier (set by block)
	Name       string   `json:"name,omitempty,omitzero"`     // human-readable name
	Source     Input    `json:"source,omitempty,omitzero"`   // input dependency
	Operations []Op     `json:"operations"`                  // build operations
	Env        []string `json:"env,omitempty,omitzero"`      // environment state
	Dir        string   `json:"dir,omitempty,omitzero"`      // working directory
	Provides   []string `json:"provides,omitempty,omitzero"` // what this stage provides
}

// Input represents stage or image dependencies
type Input struct {
	Image string     `json:"image,omitempty,omitzero"` // external image reference
	Stage string     `json:"stage,omitempty,omitzero"` // reference to another stage
	Local string     `json:"local,omitempty,omitzero"` // build context name
	URL   string     `json:"url,omitempty,omitzero"`   // HTTP/HTTPS URL for files
	Phase StagePhase `json:"phase,omitempty,omitzero"` // reference to a phase result
}

// ExportConfig represents the final runtime image configuration
type ExportConfig struct {
	Tags         []string            `json:"tags"`                    // image tags
	Labels       map[string]string   `json:"labels"`                  // image labels
	Entrypoint   []string            `json:"entrypoint,omitempty"`    // entrypoint command
	Cmd          []string            `json:"cmd,omitempty"`           // default command
	Env          []string            `json:"env,omitempty"`           // final env vars
	ExposedPorts map[string]struct{} `json:"exposed_ports,omitempty"` // port/protocol
	WorkingDir   string              `json:"working_dir,omitempty"`   // working directory
	User         string              `json:"user,omitempty"`          // user
}

// Op interface for build operations
type Op interface {
	Type() string
}

// Exec runs shell commands
type Exec struct {
	Command string  `json:"command"`          // command to execute (always uses shell)
	Mounts  []Mount `json:"mounts,omitempty"` // additional mounts needed
}

func (e Exec) Type() string { return "exec" }

// Copy copies files between stages/images
type Copy struct {
	From     Input       `json:"from"`               // source stage/image/url/local
	Src      []string    `json:"src"`                // source paths
	Dest     string      `json:"dest"`               // destination path
	Chown    string      `json:"chown,omitempty"`    // ownership
	Patterns FilePattern `json:"patterns,omitempty"` // include/exclude patterns
}

func (c Copy) Type() string { return "copy" }

// Add copies files with URL support and auto-extraction
type Add struct {
	From     Input       `json:"from,omitempty"`     // optional source stage/image/url/local
	Src      []string    `json:"src"`                // source paths/URLs
	Dest     string      `json:"dest"`               // destination path
	Chown    string      `json:"chown,omitempty"`    // ownership
	Patterns FilePattern `json:"patterns,omitempty"` // include/exclude patterns
}

func (a Add) Type() string { return "add" }

// SetEnv sets environment variables
type SetEnv struct {
	Vars map[string]string `json:"vars"` // environment variables to set
}

func (s SetEnv) Type() string { return "env" }

// Mount represents additional file system mounts for operations
type Mount struct {
	Source Input  `json:"source"` // reuse existing Input struct for mount sources
	Target string `json:"target"` // mount path in container
}

// FilePattern represents include/exclude patterns for file operations
type FilePattern struct {
	Include []string `json:"include,omitempty"` // glob patterns to include
	Exclude []string `json:"exclude,omitempty"` // glob patterns to exclude
}

// MkFile creates a file at the specified path with given data and mode
type MkFile struct {
	Dest string `json:"dest"` // destination path
	Data []byte `json:"data"` // file contents
	Mode uint32 `json:"mode"` // file mode (e.g. 0644)
}

func (m MkFile) Type() string { return "mkfile" }

// BuildContext represents a build context that can be mounted during operations
type BuildContext struct {
	Name        string            `json:"name"`         // context name for referencing
	SourceBlock string            `json:"source_block"` // which block created this context
	Description string            `json:"description"`  // human-readable description
	Metadata    map[string]string `json:"metadata"`     // debug annotations
	FS          fs.FS             `json:"-"`            // the actual filesystem (not serialized)
}

// AddStage adds a new stage to the specified phase with validation
func (p *Plan) AddStage(phaseName StagePhase, stageName, stageID string) (*Stage, error) {
	// Validate ID uniqueness
	if p.GetStage(stageID) != nil {
		return nil, fmt.Errorf("stage ID %q already exists", stageID)
	}

	// Find or create the phase
	phase := p.getOrCreatePhase(phaseName)

	stage := &Stage{
		ID:     stageID,
		Name:   stageName,
		Source: p.resolvePhaseInput(phaseName),
	}

	phase.Stages = append(phase.Stages, stage)
	return stage, nil
}

// GetStage finds a stage by ID across all phases
func (p *Plan) GetStage(id string) *Stage {
	// Check build phases
	for i := range p.BuildPhases {
		for j := range p.BuildPhases[i].Stages {
			if p.BuildPhases[i].Stages[j].ID == id {
				return p.BuildPhases[i].Stages[j]
			}
		}
	}

	// Check export phases
	for i := range p.ExportPhases {
		for j := range p.ExportPhases[i].Stages {
			if p.ExportPhases[i].Stages[j].ID == id {
				return p.ExportPhases[i].Stages[j]
			}
		}
	}

	return nil
}

// GetPhaseResult returns the result of a phase (last stage in the phase)
func (p *Plan) GetPhaseResult(phaseName StagePhase) Input {
	phase := p.getPhase(phaseName)
	if phase == nil || len(phase.Stages) == 0 {
		return Input{}
	}

	// Return the last stage in the phase
	lastStage := phase.Stages[len(phase.Stages)-1]
	return Input{Stage: lastStage.ID}
}

// HasProvider checks if a package is available from base image or build stages
func (p *Plan) HasProvider(packageName string) bool {
	// Check base image first
	if _, exists := p.BaseImage.Metadata.Packages[packageName]; exists {
		return true
	}

	// Check build stages
	for _, phase := range p.BuildPhases {
		for _, stage := range phase.Stages {
			for _, provided := range stage.Provides {
				if provided == packageName {
					return true
				}
			}
		}
	}

	return false
}

// getOrCreatePhase finds an existing phase or creates a new one
func (p *Plan) getOrCreatePhase(phaseName StagePhase) *Phase {
	// note! we're mutating the pointer here, so we need to pass it by pointer
	phases := &(p.BuildPhases)
	if phaseName.IsExportPhase() {
		phases = &(p.ExportPhases)
	}

	// Find existing phase
	for _, phase := range *phases {
		if phase.Name == phaseName {
			return phase
		}
	}

	// Create new phase
	newPhase := &Phase{
		Name:   phaseName,
		Stages: []*Stage{},
	}
	*phases = append(*phases, newPhase)
	return newPhase
}

// getPhase finds an existing phase by name
func (p *Plan) getPhase(phaseName StagePhase) *Phase {
	phases := p.BuildPhases
	if phaseName.IsExportPhase() {
		phases = p.ExportPhases
	}

	for _, phase := range phases {
		if phase.Name == phaseName {
			return phase
		}
	}

	return nil
}

// resolvePhaseInput determines the input for a new stage based on its phase
func (p *Plan) resolvePhaseInput(phaseName StagePhase) Input {
	predecessorPhase := p.getPredecessorPhase(phaseName)
	if predecessorPhase != "" {
		return p.GetPhaseResult(predecessorPhase)
	}

	// Base case - use appropriate base image
	if phaseName.IsExportPhase() {
		return Input{Image: p.BaseImage.Runtime}
	}
	return Input{Image: p.BaseImage.Build}
}

// getPredecessorPhase returns the logical predecessor phase for input resolution
func (p *Plan) getPredecessorPhase(phaseName StagePhase) StagePhase {
	switch phaseName {
	case PhaseSystemDeps:
		return PhaseBase
	case PhaseRuntime:
		return PhaseSystemDeps
	case PhaseFrameworkDeps:
		return PhaseRuntime
	case PhaseAppDeps:
		return PhaseFrameworkDeps
	case PhaseAppBuild:
		return PhaseAppDeps
	case PhaseAppSource:
		return PhaseAppBuild

	// Export phases
	case ExportPhaseRuntime:
		return ExportPhaseBase
	case ExportPhaseApp:
		return ExportPhaseRuntime
	case ExportPhaseConfig:
		return ExportPhaseApp

	default:
		return ""
	}
}

// Validate performs comprehensive validation on the plan
func (p *Plan) Validate() error {
	return ValidatePlan(p)
}

// PlanResult contains the result of plan generation along with metadata
type PlanResult struct {
	Plan     *Plan             `json:"plan"`     // the generated plan
	Metadata *PlanMetadata     `json:"metadata"` // build context and debug info
	Timing   map[string]string `json:"timing"`   // timing information (future)
}

// PlanMetadata contains build context and debug information
type PlanMetadata struct {
	Stack     string   `json:"stack"`      // e.g., "python"
	Blocks    []string `json:"blocks"`     // active block names
	BaseImage string   `json:"base_image"` // resolved base image
	Version   string   `json:"version"`    // plan schema version
}
