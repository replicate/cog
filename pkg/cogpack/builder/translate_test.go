package builder

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/client/llb/sourceresolver"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/cogpack/plan"
)

type mockImageMetaResolver struct {
	images map[string]func() (ref string, digest string, config *ocispec.ImageConfig, err error)
}

func (m *mockImageMetaResolver) ResolveImageConfig(ctx context.Context, ref string, opt sourceresolver.Opt) (string, digest.Digest, []byte, error) {
	if f, ok := m.images[ref]; ok {
		ref, retDigest, config, err := f()
		if err != nil {
			return "", "", nil, err
		}
		blob, err := json.Marshal(config)
		if err != nil {
			return "", "", nil, err
		}
		return ref, digest.Digest(retDigest), blob, nil
	}
	return "", "", nil, fmt.Errorf("image %q not found", ref)
}

func TestTranslatePlan_ResolveInput(t *testing.T) {
	t.Skip("not implemented")
	resolver := &mockImageMetaResolver{
		images: map[string]func() (string, string, *ocispec.ImageConfig, error){
			"docker.io/library/ubuntu:22.04": func() (string, string, *ocispec.ImageConfig, error) {
				return "ubuntu:22.04", "sha256:1234567890", &ocispec.ImageConfig{
					Env: []string{"PATH=/usr/bin:/bin"},
				}, nil
			},
		},
	}

	tests := []struct {
		name        string
		input       plan.Input
		stageStates map[string]llb.State
		wantErr     bool
	}{
		{
			name:    "ubuntu:22.04",
			input:   plan.Input{Scratch: true},
			wantErr: false,
		},
		// {
		// 	name:        "ubuntu:22.04",
		// 	input:       plan.Input{Image: "ubuntu:22.04"},
		// 	stageStates: map[string]llb.State{},
		// 	wantErr:     false,
		// },
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolvedState, err := resolveInput(t.Context(), tt.stageStates, ocispec.Platform{OS: "linux", Architecture: "amd64"}, resolver, tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, resolvedState)
		})
	}

}

func TestApplyMounts(t *testing.T) {
	t.Skip("dubious test, reevaluate")
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
	t.Skip("dubious test, reevaluate")
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
			name:    "scratch input",
			input:   plan.Input{Scratch: true},
			wantErr: false,
		},
		{
			name:    "empty input (invalid)",
			input:   plan.Input{},
			wantErr: true,
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
	t.Skip("dubious test, reevaluate")
	ctx := context.Background()

	// Create a test plan with mount operations
	p := &plan.Plan{
		Platform: plan.Platform{OS: "linux", Arch: "amd64"},
		Stages: []*plan.Stage{
			{
				ID:       "test-stage",
				Name:     "Test Stage",
				PhaseKey: plan.PhaseBase,
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
	}

	// Test that translation succeeds with mounts
	_, stageStates, err := TranslatePlan(ctx, p, nil)
	require.NoError(t, err)
	assert.NotNil(t, stageStates)
	assert.Contains(t, stageStates, "test-stage")
}

func TestValidateMountInput(t *testing.T) {
	t.Skip("dubious test, reevaluate")
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
	t.Skip("dubious test, reevaluate")
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

// TestTranslatePlan_EnvironmentVariablesNotInherited demonstrates the current issue
// where environment variables set in stages are not properly inherited by subsequent stages
func TestTranslatePlan_EnvironmentVariablesNotInherited(t *testing.T) {
	t.Skip("dubious test, reevaluate")
	ctx := context.Background()

	// Create a plan with two stages where the second stage should inherit env vars from the first
	p := &plan.Plan{
		Platform: plan.Platform{OS: "linux", Arch: "amd64"},
		Stages: []*plan.Stage{
			{
				ID:       "base",
				Name:     "Base with env vars",
				PhaseKey: plan.PhaseBase,
				Source:   plan.Input{Image: "ubuntu:22.04"},
				Env:      []string{"PATH=/venv/bin:/usr/bin", "PYTHONPATH=/venv/lib/python3.11/site-packages"},
				Operations: []plan.Op{
					plan.Exec{Command: "echo 'Setting up environment'"},
				},
			},
			{
				ID:       "dependent",
				Name:     "Stage that should inherit env vars",
				PhaseKey: plan.PhaseSystemDeps,
				Source:   plan.Input{Stage: "base"}, // Should inherit from previous stage
				Operations: []plan.Op{
					plan.Exec{Command: "python -c 'import os; print(os.environ.get(\"PATH\", \"PATH not found\"))'"},
				},
			},
			{
				ID:       "export",
				Name:     "Export stage",
				PhaseKey: plan.ExportPhaseBase,
				Source:   plan.Input{Image: "ubuntu:22.04"},
				Operations: []plan.Op{
					plan.Copy{
						From: plan.Input{Stage: "dependent"},
						Src:  []string{"/tmp"},
						Dest: "/tmp",
					},
				},
			},
		},
	}

	// Translate the plan
	finalState, stageStates, err := TranslatePlan(ctx, p, nil)
	require.NoError(t, err)
	assert.NotNil(t, finalState)
	assert.Contains(t, stageStates, "base")
	assert.Contains(t, stageStates, "dependent")

	// This test currently passes, but it demonstrates the problem:
	// The environment variables from the "base" stage are not available
	// in the "dependent" stage when it executes the python command.
	//
	// The current implementation:
	// 1. Applies env vars to the base state in lines 54-58 of translate.go
	// 2. But then uses llb.Diff() which loses the environment context
	// 3. The final state has the file system changes but not the env vars
	//
	// When fixed, the dependent stage should be able to access the PATH
	// and PYTHONPATH environment variables set in the base stage.
}

// TestTranslatePlan_BaseImageEnvironmentLost demonstrates that even environment
// variables from the base image are not properly preserved
func TestTranslatePlan_BaseImageEnvironmentLost(t *testing.T) {
	t.Skip("dubious test, reevaluate")
	ctx := context.Background()

	// Create a plan that starts with a base image that has environment variables
	// and then runs a command that should access those env vars
	p := &plan.Plan{
		Platform: plan.Platform{OS: "linux", Arch: "amd64"},
		Stages: []*plan.Stage{
			{
				ID:       "with-env-operation",
				Name:     "Stage with operation that should see base image env",
				PhaseKey: plan.PhaseBase,
				Source:   plan.Input{Image: "python:3.11-slim"}, // This image has PATH set
				Operations: []plan.Op{
					plan.Exec{Command: "python --version"}, // This should work with PATH from base image
				},
			},
			{
				ID:       "export",
				Name:     "Export stage",
				PhaseKey: plan.ExportPhaseBase,
				Source:   plan.Input{Image: "python:3.11-slim"},
				Operations: []plan.Op{
					plan.Copy{
						From: plan.Input{Stage: "with-env-operation"},
						Src:  []string{"/tmp"},
						Dest: "/tmp",
					},
				},
			},
		},
	}

	// Translate the plan
	finalState, stageStates, err := TranslatePlan(ctx, p, nil)
	require.NoError(t, err)
	assert.NotNil(t, finalState)
	assert.Contains(t, stageStates, "with-env-operation")

	// This test passes, but demonstrates the issue:
	// The base image python:3.11-slim has PATH set to include python,
	// but when we run operations, the environment from the base image
	// is not preserved through the llb.Diff() → llb.Copy() process.
	//
	// This is exactly the issue we're seeing with the cogpack Python stack
	// where the base image has /venv/bin in PATH but the runtime can't find python.
}

// TestTranslatePlan_MountResolution tests that mounts are properly resolved
func TestTranslatePlan_MountResolution(t *testing.T) {
	t.Skip("dubious test, reevaluate")
	plan := &plan.Plan{
		Platform: plan.Platform{OS: "linux", Arch: "amd64"},
		Contexts: map[string]*plan.BuildContext{
			"test-context": {
				Name:        "test-context",
				SourceBlock: "test",
				Description: "Test context",
			},
		},
		Stages: []*plan.Stage{
			{
				ID:       "base",
				Name:     "Base",
				PhaseKey: plan.PhaseBase,
				Source:   plan.Input{Image: "ubuntu:22.04"},
			},
			{
				ID:       "with-mount",
				Name:     "Stage with mount",
				PhaseKey: plan.PhaseSystemDeps,
				Source:   plan.Input{Stage: "base"},
				Operations: []plan.Op{
					plan.Exec{
						Command: "ls /mnt/test",
						Mounts: []plan.Mount{
							{
								Source: plan.Input{Local: "test-context"},
								Target: "/mnt/test",
							},
						},
					},
				},
			},
		},
	}

	// Translate the plan
	ctx := context.Background()
	_, stageStates, err := TranslatePlan(ctx, plan, nil)
	require.NoError(t, err)

	// Verify both stages exist
	assert.Contains(t, stageStates, "base")
	assert.Contains(t, stageStates, "with-mount")

	// The fact that translation succeeded means mount resolution worked
	// (detailed mount verification would require inspecting the LLB operations)
}

// TestTranslatePlan_ComplexStageChain tests a complex chain of stages
func TestTranslatePlan_ComplexStageChain(t *testing.T) {
	t.Skip("dubious test, reevaluate")
	plan := &plan.Plan{
		Platform: plan.Platform{OS: "linux", Arch: "amd64"},
		Stages: []*plan.Stage{
			{
				ID:       "base",
				Name:     "Base",
				PhaseKey: plan.PhaseBase,
				Source:   plan.Input{Image: "python:3.11-slim"},
				Env:      []string{"STAGE=base", "PATH=/usr/bin"},
			},
			{
				ID:       "deps",
				Name:     "Dependencies",
				PhaseKey: plan.PhaseSystemDeps,
				Source:   plan.Input{Stage: "base"},
				Env:      []string{"STAGE=deps"},
				Operations: []plan.Op{
					plan.Exec{Command: "apt-get update"},
				},
			},
			{
				ID:       "app",
				Name:     "Application",
				PhaseKey: plan.PhaseAppDeps,
				Source:   plan.Input{Stage: "deps"},
				Env:      []string{"STAGE=app"},
				Operations: []plan.Op{
					plan.Exec{Command: "pip install requests"},
				},
			},
			{
				ID:       "runtime",
				Name:     "Runtime",
				PhaseKey: plan.ExportPhaseBase,
				Source:   plan.Input{Image: "python:3.11-slim"},
				Operations: []plan.Op{
					plan.Copy{
						From: plan.Input{Stage: "app"},
						Src:  []string{"/tmp"},
						Dest: "/tmp",
					},
				},
			},
		},
	}

	// Translate the plan
	ctx := context.Background()
	_, stageStates, err := TranslatePlan(ctx, plan, nil)
	require.NoError(t, err)

	// Verify all stages exist
	for _, expectedStage := range []string{"base", "deps", "app", "runtime"} {
		assert.Contains(t, stageStates, expectedStage)
	}

	// Verify environment variable accumulation
	// Each stage should have its own STAGE variable plus inherited ones
	testCases := []struct {
		stageID          string
		expectedStageVar string
		shouldHavePath   bool
	}{
		{"base", "STAGE=base", true},
		{"deps", "STAGE=deps", true},
		{"app", "STAGE=app", true},
		{"runtime", "", false}, // runtime stage starts fresh from base image
	}

	for _, tc := range testCases {
		state := stageStates[tc.stageID]
		envList, err := state.Env(ctx)
		require.NoError(t, err)

		envArray := envList.ToArray()

		if tc.shouldHavePath {
			assert.Contains(t, envArray, "PATH=/usr/bin",
				"Stage %s should have PATH", tc.stageID)
		}

		if tc.expectedStageVar != "" {
			assert.Contains(t, envArray, tc.expectedStageVar,
				"Stage %s should have expected STAGE variable", tc.stageID)
		}
	}
}

// TestTranslatePlan_ErrorHandling tests error scenarios
func TestTranslatePlan_ErrorHandling(t *testing.T) {
	t.Skip("dubious test, reevaluate")
	tests := []struct {
		name    string
		plan    *plan.Plan
		wantErr bool
		errMsg  string
	}{
		{
			name: "nonexistent stage reference",
			plan: &plan.Plan{
				Platform: plan.Platform{OS: "linux", Arch: "amd64"},
				Stages: []*plan.Stage{
					{
						ID:       "invalid",
						Name:     "Invalid",
						PhaseKey: plan.PhaseBase,
						Source:   plan.Input{Stage: "nonexistent"},
					},
				},
			},
			wantErr: true,
			errMsg:  "unknown stage",
		},
		{
			name: "empty plan",
			plan: &plan.Plan{
				Platform: plan.Platform{OS: "linux", Arch: "amd64"},
				Stages:   []*plan.Stage{},
			},
			wantErr: true,
			errMsg:  "contained no stages",
		},
		{
			name: "invalid input",
			plan: &plan.Plan{
				Platform: plan.Platform{OS: "linux", Arch: "amd64"},
				Stages: []*plan.Stage{
					{
						ID:       "invalid",
						Name:     "Invalid",
						PhaseKey: plan.PhaseBase,
						Source:   plan.Input{}, // Empty input
					},
				},
			},
			wantErr: true,
			errMsg:  "invalid input",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := TranslatePlan(context.Background(), tt.plan, nil)

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
