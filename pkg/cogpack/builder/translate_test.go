package builder

import (
	"context"
	"testing"

	"github.com/moby/buildkit/client/llb"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/cogpack/baseimg"
	"github.com/replicate/cog/pkg/cogpack/plan"
)

func TestTranslatePlan_Basic(t *testing.T) {
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

	stg, err := p.AddStage(plan.PhaseBase, "base", "base")
	if err != nil {
		t.Fatalf("AddStage: %v", err)
	}
	stg.Source = plan.Input{Image: "ubuntu:22.04"}
	stg.Operations = []plan.Op{plan.Exec{Command: "echo hello"}}

	_, _, err = translatePlan(context.Background(), p)
	if err != nil {
		t.Fatalf("translatePlan failed: %v", err)
	}
}

func TestApplyMounts(t *testing.T) {
	platform := ocispec.Platform{OS: "linux", Architecture: "amd64"}
	stageStates := map[string]llb.State{
		"test-stage": llb.Image("ubuntu:20.04", llb.Platform(platform)),
	}
	
	// Create a test plan
	p := &plan.Plan{
		Platform: plan.Platform{OS: "linux", Arch: "amd64"},
		Contexts: map[string]*plan.BuildContext{
			"wheel-context": {
				Name: "wheel-context",
			},
		},
	}

	tests := []struct {
		name    string
		mounts  []plan.Mount
		wantErr bool
	}{
		{
			name: "local mount",
			mounts: []plan.Mount{
				{
					Source: plan.Input{Local: "wheel-context"},
					Target: "/mnt/wheel",
				},
			},
			wantErr: false,
		},
		{
			name: "stage mount",
			mounts: []plan.Mount{
				{
					Source: plan.Input{Stage: "test-stage"},
					Target: "/mnt/stage",
				},
			},
			wantErr: false,
		},
		{
			name: "image mount",
			mounts: []plan.Mount{
				{
					Source: plan.Input{Image: "ubuntu:20.04"},
					Target: "/mnt/image",
				},
			},
			wantErr: false,
		},
		{
			name: "multiple mounts",
			mounts: []plan.Mount{
				{
					Source: plan.Input{Local: "wheel-context"},
					Target: "/mnt/wheel",
				},
				{
					Source: plan.Input{Stage: "test-stage"},
					Target: "/mnt/stage",
				},
			},
			wantErr: false,
		},
		{
			name: "invalid stage mount",
			mounts: []plan.Mount{
				{
					Source: plan.Input{Stage: "nonexistent-stage"},
					Target: "/mnt/stage",
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts, err := applyMounts(tt.mounts, p, stageStates, platform)
			
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			
			require.NoError(t, err)
			assert.Len(t, opts, len(tt.mounts))
		})
	}
}

func TestResolveMountInput(t *testing.T) {
	platform := ocispec.Platform{OS: "linux", Architecture: "amd64"}
	stageStates := map[string]llb.State{
		"existing-stage": llb.Image("ubuntu:20.04", llb.Platform(platform)),
	}
	
	// Create a test plan
	p := &plan.Plan{
		Platform: plan.Platform{OS: "linux", Arch: "amd64"},
	}

	tests := []struct {
		name    string
		input   plan.Input
		wantErr bool
	}{
		{
			name:    "local input",
			input:   plan.Input{Local: "wheel-context"},
			wantErr: false,
		},
		{
			name:    "image input",
			input:   plan.Input{Image: "ubuntu:20.04"},
			wantErr: false,
		},
		{
			name:    "existing stage input",
			input:   plan.Input{Stage: "existing-stage"},
			wantErr: false,
		},
		{
			name:    "nonexistent stage input",
			input:   plan.Input{Stage: "nonexistent-stage"},
			wantErr: true,
		},
		{
			name:    "empty input (scratch)",
			input:   plan.Input{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state, err := resolveMountInput(tt.input, p, stageStates, platform)
			
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			
			require.NoError(t, err)
			assert.NotNil(t, state)
		})
	}
}

func TestTranslatePlan_WithMounts(t *testing.T) {
	ctx := context.Background()

	// Create a test plan with mount operations
	p := &plan.Plan{
		Platform: plan.Platform{OS: "linux", Arch: "amd64"},
		BaseImage: &baseimg.BaseImage{
			Build:   "ubuntu:20.04",
			Runtime: "ubuntu:20.04",
			Metadata: baseimg.BaseImageMetadata{
				Packages: map[string]baseimg.Package{},
			},
		},
		BuildPhases: []*plan.Phase{
			{
				Name: plan.PhaseAppDeps,
				Stages: []*plan.Stage{
					{
						ID:   "test-stage",
						Name: "Test Stage",
						Source: plan.Input{
							Image: "ubuntu:20.04",
						},
						Operations: []plan.Op{
							plan.Exec{
								Command: "echo 'testing mounts'",
								Mounts: []plan.Mount{
									{
										Source: plan.Input{Local: "wheel-context"},
										Target: "/mnt/wheel",
									},
								},
							},
						},
					},
				},
			},
		},
		ExportPhases: []*plan.Phase{},
	}

	// Test that translation succeeds with mounts
	_, stageStates, err := translatePlan(ctx, p)
	require.NoError(t, err)
	assert.NotNil(t, stageStates)
	assert.Contains(t, stageStates, "test-stage")
}

func TestValidateMountInput(t *testing.T) {
	platform := ocispec.Platform{OS: "linux", Architecture: "amd64"}
	stageStates := map[string]llb.State{
		"existing-stage": llb.Image("ubuntu:20.04", llb.Platform(platform)),
	}
	
	// Create a test plan
	p := &plan.Plan{
		Platform: plan.Platform{OS: "linux", Arch: "amd64"},
	}

	tests := []struct {
		name    string
		input   plan.Input
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid local input",
			input:   plan.Input{Local: "wheel-context"},
			wantErr: false,
		},
		{
			name:    "valid image input",
			input:   plan.Input{Image: "ubuntu:20.04"},
			wantErr: false,
		},
		{
			name:    "valid stage input",
			input:   plan.Input{Stage: "existing-stage"},
			wantErr: false,
		},
		{
			name:    "empty input",
			input:   plan.Input{},
			wantErr: true,
			errMsg:  "mount input must specify phase, stage, image, or local",
		},
		{
			name:    "multiple inputs",
			input:   plan.Input{Stage: "existing-stage", Image: "ubuntu:20.04"},
			wantErr: true,
			errMsg:  "mount input must specify exactly one of phase, stage, image, or local",
		},
		{
			name:    "nonexistent stage",
			input:   plan.Input{Stage: "nonexistent-stage"},
			wantErr: true,
			errMsg:  "stage \"nonexistent-stage\" does not exist",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateMountInput(tt.input, p, stageStates)
			
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

func TestApplyMounts_WithValidation(t *testing.T) {
	platform := ocispec.Platform{OS: "linux", Architecture: "amd64"}
	stageStates := map[string]llb.State{
		"existing-stage": llb.Image("ubuntu:20.04", llb.Platform(platform)),
	}
	
	// Create a test plan
	p := &plan.Plan{
		Platform: plan.Platform{OS: "linux", Arch: "amd64"},
		Contexts: map[string]*plan.BuildContext{
			"wheel-context": {
				Name: "wheel-context",
			},
		},
	}

	tests := []struct {
		name    string
		mounts  []plan.Mount
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid mounts",
			mounts: []plan.Mount{
				{
					Source: plan.Input{Local: "wheel-context"},
					Target: "/mnt/wheel",
				},
				{
					Source: plan.Input{Stage: "existing-stage"},
					Target: "/mnt/stage",
				},
			},
			wantErr: false,
		},
		{
			name: "invalid mount - nonexistent stage",
			mounts: []plan.Mount{
				{
					Source: plan.Input{Stage: "nonexistent-stage"},
					Target: "/mnt/stage",
				},
			},
			wantErr: true,
			errMsg:  "stage \"nonexistent-stage\" does not exist",
		},
		{
			name: "invalid mount - empty input",
			mounts: []plan.Mount{
				{
					Source: plan.Input{},
					Target: "/mnt/empty",
				},
			},
			wantErr: true,
			errMsg:  "mount input must specify phase, stage, image, or local",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := applyMounts(tt.mounts, p, stageStates, platform)
			
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
