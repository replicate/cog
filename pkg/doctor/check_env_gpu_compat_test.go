package doctor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/config"
)

func gpuContext(t *testing.T, gpu bool, torch string, cuda string) *CheckContext {
	t.Helper()
	cfg := &config.Config{
		Build: &config.Build{
			GPU:            gpu,
			PythonVersion:  "3.11",
			PythonPackages: []string{"torch==" + torch},
			CUDA:           cuda,
		},
	}
	// Complete populates the requirements content that TorchVersion reads; without it the
	// check skips before reaching the comparison.
	require.NoError(t, cfg.Complete(t.TempDir()))
	return &CheckContext{ctx: context.Background(), ProjectDir: t.TempDir(), Config: cfg}
}

func TestGPUCompatibilityCheck_RunsWithoutError(t *testing.T) {
	// A GPU may or may not be present in the test environment; just ensure no panic or error.
	ctx := gpuContext(t, true, "2.4.1", "12.4")
	_, err := (&GPUCompatibilityCheck{}).Check(ctx)
	require.NoError(t, err)
}

func TestGPUCompatibilityCheck_SkipsWhenDisabled(t *testing.T) {
	t.Setenv("COG_SKIP_GPU_CHECK", "1")
	ctx := gpuContext(t, true, "2.4.1", "12.4")

	findings, err := (&GPUCompatibilityCheck{}).Check(ctx)

	require.NoError(t, err)
	require.Empty(t, findings)
}

func TestGPUCompatibilityCheck_SkipsWhenGPUNotRequested(t *testing.T) {
	ctx := gpuContext(t, false, "2.4.1", "12.4")

	findings, err := (&GPUCompatibilityCheck{}).Check(ctx)

	require.NoError(t, err)
	require.Empty(t, findings)
}

func TestGPUCompatibilityCheck_SkipsWhenNoConfig(t *testing.T) {
	ctx := &CheckContext{ctx: context.Background(), ProjectDir: t.TempDir()}

	findings, err := (&GPUCompatibilityCheck{}).Check(ctx)

	require.NoError(t, err)
	require.Empty(t, findings)
}

func TestGPUCompatibilityCheck_FixReturnsNoAutoFix(t *testing.T) {
	require.ErrorIs(t, (&GPUCompatibilityCheck{}).Fix(nil, nil), ErrNoAutoFix)
}

// The comparison logic is pure; these exercise it without a GPU.

func TestEvaluateGPUCompat(t *testing.T) {
	for _, tt := range []struct {
		name       string
		capability [2]int
		torch      string
		cuda       string
		fires      bool
	}{
		// sm_120 (Blackwell consumer): needs torch>=2.7.0 AND CUDA>=12.8.
		{"sm_120 old torch fires", [2]int{12, 0}, "2.4.1", "12.4", true},
		{"sm_120 new torch passes", [2]int{12, 0}, "2.7.0", "12.8", false},
		{"sm_120 local version tag passes", [2]int{12, 0}, "2.7.1+cu128", "12.8", false},
		// Both bounds are load-bearing: torch 2.7.0+cu118 is a genuine 2.7 build with no
		// Blackwell kernels in it.
		{"sm_120 new torch but old CUDA fires", [2]int{12, 0}, "2.7.0", "11.8", true},
		{"sm_120 unset CUDA defers to torch bound", [2]int{12, 0}, "2.7.0", "", false},
		// torch listed but unpinned: pip resolves a current release; nothing to compare.
		{"unpinned torch is silent", [2]int{12, 0}, "", "", false},
		// sm_90 (Hopper): needs torch>=2.0.1.
		{"sm_90 old torch fires", [2]int{9, 0}, "1.13.1", "11.7", true},
		{"sm_90 new torch passes", [2]int{9, 0}, "2.0.1", "11.8", false},
		// An unknown newer capability falls back to the highest floor it exceeds.
		{"unknown sm_130 old torch fires", [2]int{13, 0}, "2.4.1", "12.4", true},
		{"unknown sm_130 new torch passes", [2]int{13, 0}, "2.7.0", "12.8", false},
		// Below every floor: no row, no finding.
		{"sm_86 below all floors is silent", [2]int{8, 6}, "1.13.1", "11.7", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			findings := evaluateGPUCompat(tt.capability, tt.torch, tt.cuda)
			if !tt.fires {
				require.Empty(t, findings)
				return
			}
			require.Len(t, findings, 1)
			assert.Equal(t, SeverityWarning, findings[0].Severity)
			assert.Contains(t, findings[0].Message, "torch=="+tt.torch)
			assert.Contains(t, findings[0].Message, "no kernels for")
			assert.Contains(t, findings[0].Remediation, "COG_SKIP_GPU_CHECK=1")
		})
	}
}

func TestEvaluateGPUCompat_Message(t *testing.T) {
	findings := evaluateGPUCompat([2]int{12, 0}, "2.4.1", "12.4")

	require.Len(t, findings, 1)
	assert.Contains(t, findings[0].Message, "torch==2.4.1 (CUDA 12.4) ships no kernels for sm_120")
	assert.Contains(t, findings[0].Message, "no kernel image is available for execution on the device")
	assert.Contains(t, findings[0].Remediation, "sm_120 requires torch>=2.7.0 built against CUDA>=12.8")
}

func TestFloorFor(t *testing.T) {
	for _, tt := range []struct {
		name  string
		cc    [2]int
		torch string
		cuda  string
		found bool
	}{
		{"blackwell consumer sm_120", [2]int{12, 0}, "2.7.0", "12.8", true},
		{"blackwell datacenter sm_100", [2]int{10, 0}, "2.7.0", "12.8", true},
		{"hopper sm_90", [2]int{9, 0}, "2.0.1", "11.8", true},
		// An unknown newer capability falls back to the highest floor it exceeds.
		{"unknown newer sm_130", [2]int{13, 0}, "2.7.0", "12.8", true},
		// No rows below sm_90: every probed wheel already covers Ada/Ampere/Turing, so those
		// floors cannot be bracketed. Say nothing rather than guess.
		{"ada sm_89 has no floor", [2]int{8, 9}, "", "", false},
		{"ampere sm_86 has no floor", [2]int{8, 6}, "", "", false},
		{"turing sm_75 has no floor", [2]int{7, 5}, "", "", false},
		{"pascal sm_61 has no floor", [2]int{6, 1}, "", "", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			floor, ok := floorFor(tt.cc)
			require.Equal(t, tt.found, ok)
			if tt.found {
				require.Equal(t, tt.torch, floor.MinTorch)
				require.Equal(t, tt.cuda, floor.MinCUDA)
			}
		})
	}
}

func TestParseCapability(t *testing.T) {
	for _, tt := range []struct {
		in    string
		want  [2]int
		valid bool
	}{
		{"12.0", [2]int{12, 0}, true},
		{"8.6\n", [2]int{8, 6}, true},
		{"  9.0  ", [2]int{9, 0}, true},
		{"", [2]int{}, false},
		{"not a version", [2]int{}, false},
		{"12", [2]int{}, false},
	} {
		got, ok := parseCapability(tt.in)
		require.Equal(t, tt.valid, ok, "input %q", tt.in)
		if tt.valid {
			require.Equal(t, tt.want, got, "input %q", tt.in)
		}
	}
}
