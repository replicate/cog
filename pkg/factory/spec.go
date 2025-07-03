package factory

import (
	"github.com/replicate/cog/pkg/base_images"
	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/dockerfile"
)

// BuildSpec captures the minimal information required for the first
// iteration of the experimental builder. It is intentionally small – we
// will extend it as new features are brought over from the legacy
// builder.
type BuildSpec struct {
	// Base image chosen from the CSV index. We use the DevTag for the build
	// stage and the RunTag for the runtime stage.
	BaseImage *base_images.BaseImage

	// GPU indicates whether we are targeting a GPU-enabled base image.
	GPU bool

	// PythonVersion requested by the user (e.g. "3.12"). This is
	// currently *informational* – we rely on the version baked into the
	// base image – but keeping it in the spec lets us validate or select
	// different images later.
	PythonVersion string

	// CUDAVersion is the requested CUDA version (empty for CPU builds).
	CUDAVersion string

	// Python dependency handling
	HasRequirements  bool   // true when we should install deps
	RequirementsFile string // relative path to requirements.txt inside project (if any)

	CogWheelFilename string // e.g. cog_xy.whl
}

// BuildSpecFromConfig inspects a cog config and resolves a base image
// from pkg/base_images.  The algorithm is deliberately simple for the
// first pass:
//  1. Accelerator (cpu/gpu) must match.
//  2. If CUDA version is specified, we require an exact match.
//  3. Otherwise we let ResolveBaseImage choose the newest compatible
//     image.
func BuildSpecFromConfig(cfg *config.Config) (*BuildSpec, error) {
	// Build list of constraints. The constraint type is unexported from
	// pkg/base_images, so we can only build the argument list inline.
	// First constraint: CPU/GPU accelerator.
	acceleratorConstraint := base_images.ForAccelerator(base_images.AcceleratorCPU)
	if cfg.Build != nil && cfg.Build.GPU {
		acceleratorConstraint = base_images.ForAccelerator(base_images.AcceleratorGPU)
	}

	var img *base_images.BaseImage
	var err error

	// Optional CUDA constraint (GPU only)
	if cfg.Build != nil && cfg.Build.GPU && cfg.Build.CUDA != "" {
		cudaConstraint := base_images.CudaConstraint("=" + cfg.Build.CUDA)
		img, err = base_images.ResolveBaseImage(acceleratorConstraint, cudaConstraint)
	} else {
		img, err = base_images.ResolveBaseImage(acceleratorConstraint)
	}
	if err != nil {
		return nil, err
	}

	// Determine python requirements
	hasReq := false
	reqFile := ""
	if cfg.Build != nil {
		if cfg.Build.PythonRequirements != "" {
			hasReq = true
			reqFile = cfg.Build.PythonRequirements
		} else if len(cfg.Build.PythonPackages) > 0 {
			// deprecated list – but still requires install; for now we'll
			// treat it the same (generator will need separate logic later).
			hasReq = true
		}
	}

	// Determine cog wheel filename (always present in embed)
	wheelFilename, _ := dockerfile.WheelFilename()

	spec := &BuildSpec{
		BaseImage:        img,
		GPU:              cfg.Build != nil && cfg.Build.GPU,
		PythonVersion:    cfg.Build.PythonVersion,
		CUDAVersion:      cfg.Build.CUDA,
		HasRequirements:  hasReq,
		RequirementsFile: reqFile,
		CogWheelFilename: wheelFilename,
	}
	return spec, nil
}

// (helper functions will be added in future revisions)
