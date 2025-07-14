package builder

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/cogpack/baseimg"
	"github.com/replicate/cog/pkg/cogpack/plan"
)

func TestTranslatePlan_WithPhaseInput(t *testing.T) {
	// Create a plan with multiple stages in phases
	p := &plan.Plan{
		Platform: plan.Platform{OS: "linux", Arch: "amd64"},
		BaseImage: &baseimg.BaseImage{
			Build:   "ubuntu:22.04",
			Runtime: "ubuntu:22.04",
			Metadata: baseimg.BaseImageMetadata{
				Packages: map[string]baseimg.Package{},
			},
		},
	}

	// Add stages to system-deps phase
	stage1, err := p.AddStage(plan.PhaseSystemDeps, "install-apt", "apt-install")
	require.NoError(t, err)
	stage1.Operations = []plan.Op{
		plan.Exec{Command: "apt-get update && apt-get install -y build-essential"},
	}

	stage2, err := p.AddStage(plan.PhaseSystemDeps, "install-tools", "tools-install")
	require.NoError(t, err)
	stage2.Operations = []plan.Op{
		plan.Exec{Command: "apt-get install -y git curl"},
	}

	// Add a stage to runtime phase that depends on system-deps phase
	stage3, err := p.AddStage(plan.PhaseRuntime, "python-install", "python-install")
	require.NoError(t, err)
	// This should automatically use the last stage from system-deps phase
	stage3.Operations = []plan.Op{
		plan.Exec{Command: "echo 'Installing Python'"},
	}

	// Add a stage that explicitly references the system-deps phase
	stage4, err := p.AddStage(plan.PhaseFrameworkDeps, "copy-from-phase", "copy-phase")
	require.NoError(t, err)
	stage4.Operations = []plan.Op{
		plan.Copy{
			From: plan.Input{Phase: plan.PhaseSystemDeps},
			Src:  []string{"/usr/bin/git"},
			Dest: "/opt/git",
		},
	}

	// Translate the plan
	_, stageStates, err := translatePlan(t.Context(), p)
	require.NoError(t, err)

	// Verify all stages were created
	assert.Contains(t, stageStates, "apt-install")
	assert.Contains(t, stageStates, "tools-install")
	assert.Contains(t, stageStates, "python-install")
	assert.Contains(t, stageStates, "copy-phase")

	// The phase resolution should work correctly
	// (We can't easily test the actual resolution without exposing internals,
	// but the fact that translatePlan succeeds means it worked)
}

func TestPhaseInput_Validation(t *testing.T) {
	tests := []struct {
		name    string
		setup   func() *plan.Plan
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid phase reference",
			setup: func() *plan.Plan {
				p := &plan.Plan{
					Platform: plan.Platform{OS: "linux", Arch: "amd64"},
					BaseImage: &baseimg.BaseImage{
						Build:   "ubuntu:22.04",
						Runtime: "ubuntu:22.04",
						Metadata: baseimg.BaseImageMetadata{
							Packages: map[string]baseimg.Package{},
						},
					},
				}

				// Add a stage to system-deps phase
				stage1, _ := p.AddStage(plan.PhaseSystemDeps, "stage1", "stage1")
				stage1.Operations = []plan.Op{plan.Exec{Command: "echo hello"}}

				// Add a stage that references the phase
				stage2, _ := p.AddStage(plan.PhaseRuntime, "stage2", "stage2")
				stage2.Source = plan.Input{Phase: plan.PhaseSystemDeps}
				stage2.Operations = []plan.Op{plan.Exec{Command: "echo world"}}

				return p
			},
			wantErr: false,
		},
		{
			name: "phase with no stages",
			setup: func() *plan.Plan {
				p := &plan.Plan{
					Platform: plan.Platform{OS: "linux", Arch: "amd64"},
					BaseImage: &baseimg.BaseImage{
						Build:   "ubuntu:22.04",
						Runtime: "ubuntu:22.04",
						Metadata: baseimg.BaseImageMetadata{
							Packages: map[string]baseimg.Package{},
						},
					},
				}

				// Add a stage that references an empty phase
				stage1, _ := p.AddStage(plan.PhaseRuntime, "stage1", "stage1")
				stage1.Source = plan.Input{Phase: plan.PhaseSystemDeps} // This phase has no stages
				stage1.Operations = []plan.Op{plan.Exec{Command: "echo hello"}}

				return p
			},
			wantErr: true,
			errMsg:  "has no stages",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := tt.setup()

			_, _, err := translatePlan(t.Context(), p)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestPhaseInput_InMounts(t *testing.T) {
	// Create a plan with stages in phases
	p := &plan.Plan{
		Platform: plan.Platform{OS: "linux", Arch: "amd64"},
		BaseImage: &baseimg.BaseImage{
			Build:   "ubuntu:22.04",
			Runtime: "ubuntu:22.04",
			Metadata: baseimg.BaseImageMetadata{
				Packages: map[string]baseimg.Package{},
			},
		},
		Contexts: map[string]*plan.BuildContext{
			"test-context": {
				Name: "test-context",
			},
		},
	}

	// Add a stage to system-deps phase
	stage1, err := p.AddStage(plan.PhaseSystemDeps, "build-tools", "build-tools")
	require.NoError(t, err)
	stage1.Operations = []plan.Op{
		plan.Exec{Command: "echo 'Building tools'"},
	}

	// Add a stage that mounts from a phase
	stage2, err := p.AddStage(plan.PhaseRuntime, "use-tools", "use-tools")
	require.NoError(t, err)
	stage2.Operations = []plan.Op{
		plan.Exec{
			Command: "ls /mnt/tools",
			Mounts: []plan.Mount{
				{
					Source: plan.Input{Phase: plan.PhaseSystemDeps},
					Target: "/mnt/tools",
				},
			},
		},
	}

	// Should translate successfully
	_, _, err = translatePlan(t.Context(), p)
	assert.NoError(t, err)
}
