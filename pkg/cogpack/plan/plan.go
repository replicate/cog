package plan

import (
	"fmt"
	"io/fs"
)

// Package cogpack implements the Stack → Blocks → Plan → Builder architecture
// described in cogpack.claude.md. See "Core Components" section for component
// responsibilities and "Design Reasoning" for architectural decisions.

// Platform represents the target platform for the build
type Platform struct {
	OS   string `json:"os"`   // "linux"
	Arch string `json:"arch"` // "amd64"
}

// Plan represents the complete build specification that can be executed by a builder
type Plan struct {
	Platform     Platform                 `json:"platform"` // linux/amd64
	BuildStages  []*Stage                 `json:"buildStages"`
	ExportStages []*Stage                 `json:"exportStages"`
	Export       *ExportConfig            `json:"export"`   // final image config
	Contexts     map[string]*BuildContext `json:"contexts"` // build contexts for mounting
}

// Stage represents a build phase with explicit state management
type Stage struct {
	ID         string   `json:"id"`                          // unique identifier (set by block)
	Name       string   `json:"name,omitempty,omitzero"`     // human-readable name
	Source     Input    `json:"source,omitempty,omitzero"`   // input dependency
	Operations []Op     `json:"operations,omitempty"`        // build operations
	Env        []string `json:"env,omitempty,omitzero"`      // environment state
	Dir        string   `json:"dir,omitempty,omitzero"`      // working directory
	Provides   []string `json:"provides,omitempty,omitzero"` // what this stage provides
}

func (s *Stage) Validate() error {
	if err := s.Source.Validate(); err != nil {
		return fmt.Errorf("stage %q input validation failed: %w", s.ID, err)
	}

	return nil
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

// Mount represents additional file system mounts for operations
type Mount struct {
	Source Input  `json:"source"` // reuse existing Input struct for mount sources
	Target string `json:"target"` // mount path in container
}

// BuildContext represents a build context that can be mounted during operations
type BuildContext struct {
	Name        string            `json:"name"`         // context name for referencing
	SourceBlock string            `json:"source_block"` // which block created this context
	Description string            `json:"description"`  // human-readable description
	Metadata    map[string]string `json:"metadata"`     // debug annotations
	FS          fs.FS             `json:"-"`            // the actual filesystem (not serialized)
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
