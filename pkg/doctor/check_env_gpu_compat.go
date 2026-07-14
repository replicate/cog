package doctor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"github.com/replicate/cog/pkg/util/version"
)

// GPUFloor is the oldest torch (and CUDA) release that ships executable kernels for a
// given compute capability.
//
// CUDA cubins are binary-compatible only within a compute capability major; crossing majors
// requires embedded PTX for the driver to JIT, which older torch wheels do not ship. Both
// bounds matter: torch 2.7.0+cu118 is a genuine 2.7 build with no Blackwell kernels.
type GPUFloor struct {
	MinTorch string
	MinCUDA  string
}

// gpuFloors maps compute capability (major, minor) to its floor. Lookup takes the highest
// entry the device meets or exceeds; a device below every entry yields no finding.
//
// Measured by installing each wheel and reading torch._C._cuda_getArchFlags(), which works
// without a GPU. Each floor is bracketed: the named version ships kernels for the
// capability, the release below it does not.
//
//	torch 2.7.0+cu128   cubins: sm_75 sm_80 sm_86 sm_90 sm_100 sm_120   PTX: compute_120
//	torch 2.6.0+cu126   cubins: sm_50 ... sm_90                         PTX: none
//	torch 2.4.1+cu124   cubins: sm_50 ... sm_90                         PTX: none
//	torch 2.0.1+cu118   cubins: sm_37 ... sm_90                         PTX: none
//	torch 1.13.1+cu117  cubins: sm_37 ... sm_86                         PTX: none
//
// No rows below sm_90: every probed wheel already covers Turing/Ampere/Ada, so those floors
// cannot be bracketed. When extending, bracket the new row the same way rather than guess.
//
// sm_89 (Ada) appears in no cubin list yet RTX 40xx works: sm_86 cubins run on sm_89 (same
// major). sm_120 gets no such fallback from sm_90.
var gpuFloors = map[[2]int]GPUFloor{
	{12, 0}: {MinTorch: "2.7.0", MinCUDA: "12.8"}, // Blackwell consumer, RTX 50xx
	{10, 0}: {MinTorch: "2.7.0", MinCUDA: "12.8"}, // Blackwell datacenter, B100/B200
	{9, 0}:  {MinTorch: "2.0.1", MinCUDA: "11.8"}, // Hopper, H100
}

// GPUCompatibilityCheck verifies that the resolved torch/CUDA versions can execute on this
// machine's GPU. Cog derives CUDA from the framework pin without consulting the device, so
// a build can succeed and produce an image where every CUDA operation fails at runtime with
// "no kernel image is available for execution on the device".
type GPUCompatibilityCheck struct{}

func (c *GPUCompatibilityCheck) Name() string        { return "env-gpu-compat" }
func (c *GPUCompatibilityCheck) Group() Group        { return GroupEnvironment }
func (c *GPUCompatibilityCheck) Description() string { return "GPU compatibility" }

func (c *GPUCompatibilityCheck) Check(ctx *CheckContext) ([]Finding, error) {
	if os.Getenv("COG_SKIP_GPU_CHECK") != "" {
		return nil, nil
	}
	if ctx.Config == nil || !ctx.Config.Build.GPU {
		return nil, nil
	}

	torchVersion, hasTorch := ctx.Config.TorchVersion()
	if !hasTorch {
		return nil, nil
	}

	capability, ok := detectComputeCapability(ctx.ctx)
	if !ok {
		// No GPU on this machine (or no driver); nothing to compare against.
		return nil, nil
	}

	return evaluateGPUCompat(capability, torchVersion, ctx.Config.Build.CUDA), nil
}

// evaluateGPUCompat compares a resolved torch/CUDA pair against the floor for the given
// compute capability. Kept free of I/O so it is testable without a GPU.
func evaluateGPUCompat(capability [2]int, torchVersion string, cuda string) []Finding {
	floor, ok := floorFor(capability)
	if !ok {
		// Older than any floor we know; say nothing rather than guess.
		return nil
	}

	if torchVersion == "" {
		// torch listed but unpinned; pip resolves a current release. Nothing to compare.
		return nil
	}

	torchOK := version.GreaterOrEqual(torchVersion, floor.MinTorch)
	cudaOK := cuda == "" || version.GreaterOrEqual(cuda, floor.MinCUDA)
	if torchOK && cudaOK {
		return nil
	}

	sm := fmt.Sprintf("sm_%d%d", capability[0], capability[1])
	// Warning, not error: the image is valid and runs on other hardware; it only fails on
	// this machine's GPU (same taxonomy as PythonVersionCheck).
	return []Finding{{
		Severity: SeverityWarning,
		Message: fmt.Sprintf(
			"torch==%s (CUDA %s) ships no kernels for %s, the compute capability of this machine's GPU. "+
				"The image will build, but every CUDA operation in it will fail at runtime with "+
				"\"no kernel image is available for execution on the device\".",
			torchVersion, cudaDisplay(cuda), sm,
		),
		Remediation: fmt.Sprintf(
			"%s requires torch>=%s built against CUDA>=%s. Pin a newer torch, or set "+
				"COG_SKIP_GPU_CHECK=1 if you are building for a different GPU than the one in this machine.",
			sm, floor.MinTorch, floor.MinCUDA,
		),
	}}
}

func (c *GPUCompatibilityCheck) Fix(_ *CheckContext, _ []Finding) error {
	return ErrNoAutoFix
}

func cudaDisplay(cuda string) string {
	if cuda == "" {
		return "unset"
	}
	return cuda
}

// detectComputeCapability returns the lowest compute capability among the machine's GPUs:
// the image has to run on the weakest device present.
func detectComputeCapability(ctx context.Context) ([2]int, bool) {
	execCtx, cancel := context.WithTimeout(ctx, envCheckTimeout)
	defer cancel()

	out, err := exec.CommandContext(execCtx,
		"nvidia-smi", "--query-gpu=compute_cap", "--format=csv,noheader").Output()
	if err != nil {
		return [2]int{}, false
	}

	var caps [][2]int
	for line := range strings.SplitSeq(string(out), "\n") {
		if c, ok := parseCapability(line); ok {
			caps = append(caps, c)
		}
	}
	if len(caps) == 0 {
		return [2]int{}, false
	}

	sort.Slice(caps, func(i, j int) bool {
		if caps[i][0] != caps[j][0] {
			return caps[i][0] < caps[j][0]
		}
		return caps[i][1] < caps[j][1]
	})
	return caps[0], true
}

// parseCapability parses a single nvidia-smi compute_cap value, e.g. "12.0".
func parseCapability(s string) ([2]int, bool) {
	majorStr, minorStr, found := strings.Cut(strings.TrimSpace(s), ".")
	if !found {
		return [2]int{}, false
	}
	major, err := strconv.Atoi(majorStr)
	if err != nil {
		return [2]int{}, false
	}
	minor, err := strconv.Atoi(minorStr)
	if err != nil {
		return [2]int{}, false
	}
	return [2]int{major, minor}, true
}

// floorFor returns the floor for the highest table entry the device meets or exceeds.
func floorFor(capability [2]int) (GPUFloor, bool) {
	keys := make([][2]int, 0, len(gpuFloors))
	for k := range gpuFloors {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i][0] != keys[j][0] {
			return keys[i][0] > keys[j][0]
		}
		return keys[i][1] > keys[j][1]
	})

	for _, k := range keys {
		if capability[0] > k[0] || (capability[0] == k[0] && capability[1] >= k[1]) {
			return gpuFloors[k], true
		}
	}
	return GPUFloor{}, false
}
