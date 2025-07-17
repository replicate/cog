//go:build ignore

package factory

import (
	"fmt"

	"github.com/hashicorp/go-version"

	"github.com/replicate/cog/pkg/base_images"
	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/dockerfile"
)

// BuildSpec captures the minimal information required for the first
// iteration of the experimental builder. It is intentionally small – we
// will extend it as new features are brought over from the legacy
// builder.
type BuildSpec struct {
	Env *BuildEnv

	// Base image chosen from the CSV index. We use the DevTag for the build
	// stage and the RunTag for the runtime stage.
	BaseImage *base_images.BaseImage

	// GPU indicates whether we are targeting a GPU-enabled base image.
	GPU bool

	// PythonVersion requested by the user (e.g. "3.12"). This is
	// currently *informational* – we rely on the version baked into the
	// base image – but keeping it in the spec lets us validate or select
	// different images later.
	PythonVersion *version.Version

	// CUDAVersion is the requested CUDA version (empty for CPU builds).
	CUDAVersion *version.Version

	// Python dependency handling
	HasRequirements  bool   // true when we should install deps
	RequirementsFile string // relative path to requirements.txt inside project (if any)

	CogWheelFilename string // e.g. cog_xy.whl

	// SystemPackages lists additional OS packages to install via apt.
	SystemPackages []string

	PythonRequirements *PythonRequirements
}

type Step interface {
	DevOps() []BuildOp
	RunOps() []BuildOp
}

type BuildOp func(stage *stage)

// func (f *Factory) determineBaseImage(cfg *config.Config) (*base_images.BaseImage, error) {
// 	constraints := []base_images.Constraint{}

// 	if cfg.Build != nil && cfg.Build.PythonVersion != "" {
// 		constraints = append(constraints, base_images.PythonConstraint(cfg.Build.PythonVersion))
// 	}

// 	return nil, nil
// }

// func (f *Factory) determineCudaVersionConstraint(cfg *config.Config) (string, error) {
// 	if cfg.Build != nil && cfg.Build.CUDA != "" {
// 		return cfg.Build.CUDA, nil
// 	}
// 	return cfg.CudaVersionConstraint()
// }

func (f *Factory) solvePythonRequirements(cfg *config.Config) (*PythonRequirements, error) {
	packages, err := cfg.PythonPackages()
	if err != nil {
		return nil, err
	}

	pythonRequirements := &PythonRequirements{
		Requirements: packages,
	}

	// hasReq := false
	// reqFile := ""

	// if cfg.Build != nil {
	// 	if cfg.Build.PythonRequirements != "" {
	// 		hasReq = true
	// 		reqFile = cfg.Build.PythonRequirements
	// 	}
	// }
	// return nil, nil
	return pythonRequirements, nil
}

func (f *Factory) solveBuildSpec(env *BuildEnv) (*BuildSpec, error) {
	img, err := f.solveBaseImage(env.Config)
	if err != nil {
		return nil, err
	}

	pythonRequirements, err := f.solvePythonRequirements(env.Config)
	if err != nil {
		return nil, err
	}

	wheelFilename, err := dockerfile.WheelFilename()
	if err != nil {
		return nil, err
	}

	return &BuildSpec{
		Env:                env,
		BaseImage:          img,
		GPU:                env.Config.Build != nil && env.Config.Build.GPU,
		PythonVersion:      img.PythonVersion,
		CUDAVersion:        img.CudaVersion,
		HasRequirements:    env.Config.Build.PythonRequirements != "",
		RequirementsFile:   env.Config.Build.PythonRequirements,
		CogWheelFilename:   wheelFilename,
		SystemPackages:     env.Config.Build.SystemPackages,
		PythonRequirements: pythonRequirements,
	}, nil
}

// func (f *Factory) solveBuildOps(spec *BuildSpec) (, error) {
// 	plan := &BuildPlan{}

// 	// install apt packages

// 	return plan, nil
// }

func (f *Factory) solveBaseImage(cfg *config.Config) (*base_images.BaseImage, error) {
	var baseImageConstraints []base_images.Constraint

	pythonVer, err := cfg.PythonVersionConstraint()
	if err != nil {
		return nil, err
	}
	if pythonVer != "" {
		fmt.Println("pythonVer", pythonVer)
		baseImageConstraints = append(baseImageConstraints, base_images.PythonConstraint(pythonVer))
	}

	if cfg.Build != nil && cfg.Build.GPU {
		baseImageConstraints = append(baseImageConstraints, base_images.ForAccelerator(base_images.AcceleratorGPU))

		cudaVer, err := cfg.CudaVersionConstraint()
		if err != nil {
			return nil, err
		}
		if cudaVer != "" {
			// hack to make sure we get a base image from our tiny sample set
			if cudaVer != "12.8" {
				cudaVer = "~>12.8.0"
			}
			baseImageConstraints = append(baseImageConstraints, base_images.CudaConstraint(cudaVer))
		}

	} else {
		baseImageConstraints = append(baseImageConstraints, base_images.ForAccelerator(base_images.AcceleratorCPU))
	}

	img, err := base_images.ResolveBaseImage(baseImageConstraints...)
	if err != nil {
		return nil, err
	}
	fmt.Println("resolved base image", img)

	return img, nil
}

type InstallPytorch struct {
}

// // BuildSpecFromConfig inspects a cog config and resolves a base image
// // from pkg/base_images.  The algorithm is deliberately simple for the
// // first pass:
// //  1. Accelerator (cpu/gpu) must match.
// //  2. If CUDA version is specified, we require an exact match.
// //  3. Otherwise we let ResolveBaseImage choose the newest compatible
// //     image.
// func (f *Factory) BuildSpecFromConfig(cfg *config.Config) (*BuildSpec, error) {
// 	// Build list of constraints. The constraint type is unexported from
// 	// pkg/base_images, so we can only build the argument list inline.
// 	// First constraint: CPU/GPU accelerator.
// 	acceleratorConstraint := base_images.ForAccelerator(base_images.AcceleratorCPU)
// 	if cfg.Build != nil && cfg.Build.GPU {
// 		acceleratorConstraint = base_images.ForAccelerator(base_images.AcceleratorGPU)
// 	}

// 	cudaVer, err := cfg.CudaVersionConstraint()
// 	if err != nil {
// 		return nil, err
// 	}
// 	fmt.Println("cudaVer", cudaVer)

// 	var img *base_images.BaseImage

// 	// Optional CUDA constraint (GPU only)
// 	if cfg.Build != nil && cfg.Build.GPU && cfg.Build.CUDA != "" {
// 		cudaConstraint := base_images.CudaConstraint("=" + cfg.Build.CUDA)
// 		img, err = base_images.ResolveBaseImage(acceleratorConstraint, cudaConstraint)
// 	} else {
// 		img, err = base_images.ResolveBaseImage(acceleratorConstraint)
// 	}
// 	if err != nil {
// 		return nil, err
// 	}

// 	// Determine python requirements
// 	hasReq := false
// 	reqFile := ""
// 	if cfg.Build != nil {
// 		if cfg.Build.PythonRequirements != "" {
// 			hasReq = true
// 			reqFile = cfg.Build.PythonRequirements
// 		} else if len(cfg.Build.PythonPackages) > 0 {
// 			// deprecated list – but still requires install; for now we'll
// 			// treat it the same (generator will need separate logic later).
// 			hasReq = true
// 		}
// 	}

// 	// Determine cog wheel filename (always present in embed)
// 	wheelFilename, _ := dockerfile.WheelFilename()

// 	spec := &BuildSpec{
// 		BaseImage:        img,
// 		GPU:              cfg.Build != nil && cfg.Build.GPU,
// 		PythonVersion:    cfg.Build.PythonVersion,
// 		CUDAVersion:      cfg.Build.CUDA,
// 		HasRequirements:  hasReq,
// 		RequirementsFile: reqFile,
// 		CogWheelFilename: wheelFilename,
// 		SystemPackages: func() []string {
// 			if cfg.Build != nil {
// 				return cfg.Build.SystemPackages
// 			}
// 			return nil
// 		}(),
// 	}
// 	return spec, nil
// }

// (helper functions will be added in future revisions)
