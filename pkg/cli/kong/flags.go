package kong

import (
	"fmt"
	"strings"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/model"
)

// BuildFlags are shared across commands that build images.
type BuildFlags struct {
	Tag             string   `help:"A name for the built image in the form 'repository:tag'" short:"t"`
	Progress        string   `help:"Build progress output type: auto, tty, plain, quiet" default:"${progress_default}" name:"progress"`
	Secrets         []string `help:"Secrets in the form 'id=foo,src=/path/to/file'" name:"secret"`
	NoCache         bool     `help:"Do not use cache when building the image" name:"no-cache"`
	SeparateWeights bool     `help:"Separate model weights from code in image layers" name:"separate-weights"`
	SchemaFile      string   `help:"Load OpenAPI schema from a file" name:"openapi-schema"`
	UseCudaBase     string   `help:"Use Nvidia CUDA base image, 'true' or 'false'" name:"use-cuda-base-image" default:"auto"`
	Dockerfile      string   `help:"Path to a Dockerfile" name:"dockerfile" hidden:""`
	UseCogBase      bool     `help:"Use pre-built Cog base image for faster cold boots" name:"use-cog-base-image" default:"true" negatable:""`
	Timestamp       int64    `help:"Epoch seconds for reproducible builds (-1 to disable)" name:"timestamp" hidden:"" default:"-1"`
	Strip           bool     `help:"Strip shared libraries" name:"strip" hidden:""`
	Precompile      bool     `help:"Precompile python files" name:"precompile" hidden:""`
	ConfigFile      string   `help:"Config file path" short:"f" name:"file" default:"cog.yaml"`
}

// AfterApply syncs the timestamp to the global used by business logic.
func (f *BuildFlags) AfterApply() error {
	config.BuildSourceEpochTimestamp = f.Timestamp
	return nil
}

// Validate checks mutually exclusive build mode flags.
func (f *BuildFlags) Validate() error {
	var set []string
	if f.UseCudaBase != "auto" {
		set = append(set, "--use-cuda-base-image")
	}
	if f.Dockerfile != "" {
		set = append(set, "--dockerfile")
	}
	// UseCogBase default is true; if false, it was explicitly set.
	if !f.UseCogBase {
		set = append(set, "--use-cog-base-image")
	}
	if len(set) > 1 {
		return fmt.Errorf("The flags %s are mutually exclusive: you can only set one of them.", strings.Join(set, " and "))
	}
	return nil
}

// BuildOptions converts flags to model.BuildOptions.
func (f *BuildFlags) BuildOptions(imageName string, annotations map[string]string) model.BuildOptions {
	return model.BuildOptions{
		ImageName:        imageName,
		Secrets:          f.Secrets,
		NoCache:          f.NoCache,
		SeparateWeights:  f.SeparateWeights,
		UseCudaBaseImage: f.UseCudaBase,
		ProgressOutput:   f.Progress,
		SchemaFile:       f.SchemaFile,
		DockerfileFile:   f.Dockerfile,
		UseCogBaseImage:  f.determineCogBase(),
		Strip:            f.Strip,
		Precompile:       f.Precompile,
		Annotations:      annotations,
		ImageFormat:      model.ImageFormatFromEnv(),
	}
}

// BuildBaseOptions converts flags for dev-mode base image builds.
func (f *BuildFlags) BuildBaseOptions() model.BuildBaseOptions {
	return model.BuildBaseOptions{
		UseCudaBaseImage: f.UseCudaBase,
		UseCogBaseImage:  f.determineCogBase(),
		ProgressOutput:   f.Progress,
		RequiresCog:      true,
	}
}

// determineCogBase returns nil when the flag wasn't explicitly changed from
// default, matching the Cobra behavior where nil means "use server default."
// Kong doesn't track "changed" natively, so we use negatable + default:true
// and treat false as explicitly set.
func (f *BuildFlags) determineCogBase() *bool {
	if f.UseCogBase {
		// Default value — treat as not explicitly set.
		return nil
	}
	v := f.UseCogBase
	return &v
}

// GPUFlags are shared across commands that run containers with optional GPU.
type GPUFlags struct {
	GPUs string `help:"GPU devices, same format as docker run --gpus" name:"gpus"`
}

// ResolveGPUs returns the effective GPU string, auto-detecting from model config.
func (f *GPUFlags) ResolveGPUs(hasGPU bool) string {
	if f.GPUs != "" {
		return f.GPUs
	}
	if hasGPU {
		return "all"
	}
	return ""
}

// IsAutoDetected returns true if the GPU was not explicitly set by the user.
func (f *GPUFlags) IsAutoDetected() bool {
	return f.GPUs == ""
}
