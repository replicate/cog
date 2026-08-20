package doctor

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/config"
)

func gpuContext(t *testing.T, gpu bool, torch string, cuda string) *CheckContext {
	t.Helper()
	return gpuContextRaw(t, gpu, "torch=="+torch, cuda)
}

// gpuContextRaw builds a context from a raw torch requirement line, so tests can exercise
// ranges and unpinned requirements that gpuContext's "torch==<v>" shorthand cannot express.
func gpuContextRaw(t *testing.T, gpu bool, torchReq string, cuda string) *CheckContext {
	t.Helper()
	cfg := &config.Config{
		Build: &config.Build{
			GPU:            gpu,
			PythonVersion:  "3.11",
			PythonPackages: []string{torchReq},
			CUDA:           cuda,
		},
	}
	// Complete populates the requirements content that ResolvedTorchWheel reads; without it
	// the check skips before reaching the comparison.
	require.NoError(t, cfg.Complete(t.TempDir()))
	return &CheckContext{ctx: context.Background(), ProjectDir: t.TempDir(), Config: cfg}
}

// stubComputeCaps replaces the nvidia-smi probe with fixed output for the test's duration,
// so the production detection path runs deterministically without a GPU.
func stubComputeCaps(t *testing.T, out string, err error) {
	t.Helper()
	prev := queryComputeCaps
	queryComputeCaps = func(context.Context) ([]byte, error) { return []byte(out), err }
	t.Cleanup(func() { queryComputeCaps = prev })
}

func TestGPUCompatibilityCheck_FiresForIncompatibleGPU(t *testing.T) {
	stubComputeCaps(t, "12.0\n", nil)
	ctx := gpuContext(t, true, "2.4.1", "12.4")

	findings, err := (&GPUCompatibilityCheck{}).Check(ctx)

	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.Contains(t, findings[0].Message, "sm_120")
}

func TestGPUCompatibilityCheck_PassesForCompatibleGPU(t *testing.T) {
	stubComputeCaps(t, "12.0\n", nil)
	ctx := gpuContext(t, true, "2.7.0", "12.8")

	findings, err := (&GPUCompatibilityCheck{}).Check(ctx)

	require.NoError(t, err)
	require.Empty(t, findings)
}

// A host with an old-but-supported GPU and a newer unsupported one must still warn: the
// numerically lowest device (sm_90) clears torch 2.4.1's floor but sm_120 does not.
func TestGPUCompatibilityCheck_MixedGPUsFireForStrictestFloor(t *testing.T) {
	stubComputeCaps(t, "9.0\n12.0\n", nil)
	ctx := gpuContext(t, true, "2.4.1", "12.4")

	findings, err := (&GPUCompatibilityCheck{}).Check(ctx)

	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.Contains(t, findings[0].Message, "sm_120")
	assert.NotContains(t, findings[0].Message, "sm_90")
}

func TestGPUCompatibilityCheck_SkipsWhenNvidiaSMIFails(t *testing.T) {
	stubComputeCaps(t, "", errors.New("nvidia-smi: command not found"))
	ctx := gpuContext(t, true, "2.4.1", "12.4")

	findings, err := (&GPUCompatibilityCheck{}).Check(ctx)

	require.NoError(t, err)
	require.Empty(t, findings)
}

func TestGPUCompatibilityCheck_SkipsWhenOutputMalformed(t *testing.T) {
	stubComputeCaps(t, "N/A\n[Insufficient Permissions]\n", nil)
	ctx := gpuContext(t, true, "2.4.1", "12.4")

	findings, err := (&GPUCompatibilityCheck{}).Check(ctx)

	require.NoError(t, err)
	require.Empty(t, findings)
}

// A non-exact pin lets pip resolve any release, so the check must not warn on the token even
// when the detected GPU would fail that token as an exact version.
func TestGPUCompatibilityCheck_SkipsNonExactPin(t *testing.T) {
	stubComputeCaps(t, "12.0\n", nil)
	ctx := gpuContextRaw(t, true, "torch<2.7.0", "12.4")

	findings, err := (&GPUCompatibilityCheck{}).Check(ctx)

	require.NoError(t, err)
	require.Empty(t, findings)
}

// The check must evaluate the wheel Cog installs, not the requirement's local tag. A
// torch==2.7.0+cu118 pin with cuda: "12.8" resolves to the cu128 index (that wheel has
// Blackwell kernels), so the check must stay silent rather than warn on the discarded +cu118.
func TestGPUCompatibilityCheck_LocalTagResolvedAgainstBuildCUDA(t *testing.T) {
	stubComputeCaps(t, "12.0\n", nil)
	ctx := gpuContextRaw(t, true, "torch==2.7.0+cu118", "12.8")

	findings, err := (&GPUCompatibilityCheck{}).Check(ctx)

	require.NoError(t, err)
	require.Empty(t, findings)
}

// The mirror case: an explicit cu118 index installs the cu118 wheel regardless of cuda: "12.8",
// and that wheel has no Blackwell kernels, so the check must fire against the resolved 11.8.
func TestGPUCompatibilityCheck_ExplicitIndexOverridesBuildCUDA(t *testing.T) {
	stubComputeCaps(t, "12.0\n", nil)
	ctx := gpuContextRaw(t, true, "torch==2.7.0 --extra-index-url=https://download.pytorch.org/whl/cu118", "12.8")

	findings, err := (&GPUCompatibilityCheck{}).Check(ctx)

	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.Contains(t, findings[0].Message, "sm_120")
	// The message reports the wheel's CUDA (11.8), not the base image's 12.8.
	assert.Contains(t, findings[0].Message, "CUDA 11.8")
}

// Full Config.Complete -> Check regression: extras and casing must not hide the torch pin.
// Before name normalization, ResolvedTorchWheel skipped these and the check said nothing.
func TestGPUCompatibilityCheck_NormalizesTorchName(t *testing.T) {
	for _, torchReq := range []string{"torch[extra]==2.4.1", "Torch==2.4.1"} {
		t.Run(torchReq, func(t *testing.T) {
			stubComputeCaps(t, "12.0\n", nil)
			ctx := gpuContextRaw(t, true, torchReq, "12.4")

			findings, err := (&GPUCompatibilityCheck{}).Check(ctx)

			require.NoError(t, err)
			require.Len(t, findings, 1)
			assert.Contains(t, findings[0].Message, "sm_120")
		})
	}
}

func TestGPUCompatibilityCheck_RegisteredInRunner(t *testing.T) {
	var registered bool
	for _, c := range AllChecks() {
		if _, ok := c.(*GPUCompatibilityCheck); ok {
			registered = true
			break
		}
	}
	require.True(t, registered, "GPUCompatibilityCheck should be registered in AllChecks")
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

func TestGPUCompatibilityCheck_SkipsWhenNoBuildSection(t *testing.T) {
	ctx := &CheckContext{ctx: context.Background(), ProjectDir: t.TempDir(), Config: &config.Config{}}

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
		// Exactly at the floor with a local tag: the +cu128 modifier must not read as below
		// 2.7.0. Regression for version.GreaterOrEqual folding the local tag into equality.
		{"sm_120 exact floor with local tag passes", [2]int{12, 0}, "2.7.0+cu128", "12.8", false},
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

func TestEvaluateGPUCompatAll(t *testing.T) {
	t.Run("distinct capabilities each yield a finding", func(t *testing.T) {
		findings := evaluateGPUCompatAll([][2]int{{10, 0}, {12, 0}}, "2.4.1", "12.4")
		require.Len(t, findings, 2)
	})
	t.Run("a passing capability suppresses its finding", func(t *testing.T) {
		findings := evaluateGPUCompatAll([][2]int{{9, 0}, {12, 0}}, "2.4.1", "12.4")
		require.Len(t, findings, 1)
		assert.Contains(t, findings[0].Message, "sm_120")
	})
	t.Run("all-compatible capabilities yield nothing", func(t *testing.T) {
		findings := evaluateGPUCompatAll([][2]int{{9, 0}, {12, 0}}, "2.7.0", "12.8")
		require.Empty(t, findings)
	})
	t.Run("duplicate capabilities are evaluated once", func(t *testing.T) {
		findings := evaluateGPUCompatAll([][2]int{{12, 0}, {12, 0}}, "2.4.1", "12.4")
		require.Len(t, findings, 1)
	})
}

func TestParseComputeCapabilities(t *testing.T) {
	for _, tt := range []struct {
		name string
		out  string
		want [][2]int
	}{
		{"single", "12.0\n", [][2]int{{12, 0}}},
		{"multiple sorted ascending", "12.0\n9.0\n", [][2]int{{9, 0}, {12, 0}}},
		{"duplicates collapsed", "12.0\n12.0\n", [][2]int{{12, 0}}},
		{"malformed lines skipped", "N/A\n12.0\ngarbage\n", [][2]int{{12, 0}}},
		{"all malformed yields none", "N/A\n[Insufficient Permissions]\n", nil},
		{"empty yields none", "", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, parseComputeCapabilities(tt.out))
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
