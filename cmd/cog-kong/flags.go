package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/replicate/cog/pkg/cli"
	"github.com/replicate/cog/pkg/model"
)

// ConfigFlag is an embeddable flag group for specifying the cog.yaml path.
// Any command that embeds ConfigFlag (directly or via BuildFlags) automatically
// gets a ProvideModelSource method discovered by Kong's DI system.
type ConfigFlag struct {
	File string `name:"file" short:"f" default:"cog.yaml" help:"The name of the config file."`
}

// ProvideModelSource is discovered by Kong's DI system (Provide* convention).
// It loads the model source from the config file path specified by --file.
func (c *ConfigFlag) ProvideModelSource() (*model.Source, error) {
	return model.NewSource(c.File)
}

// BuildFlags groups the image-building flags shared by the build and push
// commands. Build-only flags (timestamp, skip-schema-validation) live on the
// BuildCmd struct itself so they don't leak onto push, matching the Cobra CLI.
type BuildFlags struct {
	ConfigFlag `embed:""`

	NoCache          bool     `name:"no-cache" help:"Do not use cache when building the image."`
	SeparateWeights  bool     `name:"separate-weights" help:"Separate model weights from code in image layers."`
	Secrets          []string `name:"secret" help:"Secrets to pass to the build environment in the form 'id=foo,src=/path/to/file'."`
	Progress         string   `name:"progress" default:"${progress_default}" enum:"auto,plain,tty,quiet" help:"Set type of build progress output: ${enum}."`
	UseCudaBaseImage string   `name:"use-cuda-base-image" default:"auto" enum:"auto,true,false" help:"Use Nvidia CUDA base image, 'true' (default) or 'false' (use python base image)."`
	UseCogBaseImage  *bool    `name:"use-cog-base-image" help:"Use pre-built Cog base image for faster cold boots."`
	// Cobra accepts --openapi-schema as a plain string and defers the
	// file-existence check to build time, so do not use type:"existingfile".
	OpenAPISchema string `name:"openapi-schema" help:"Load OpenAPI schema from a file."`

	// Hidden flags
	// Cobra accepts --dockerfile as a plain string and defers the
	// file-existence check to build time, so do not use type:"existingfile".
	Dockerfile string `name:"dockerfile" hidden:"" help:"Path to a Dockerfile. If set, cog will use this Dockerfile instead of generating one from cog.yaml."`
	Strip      bool   `name:"strip" hidden:"" help:"Whether to strip shared libraries for faster inference times."`
	Precompile bool   `name:"precompile" hidden:"" help:"Whether to precompile python files for faster load times."`
}

// Options converts the Kong build flags into the parser-independent
// cli.BuildFlagsOptions shared with the Cobra CLI. Timestamp defaults to -1
// (timestamp rewriting disabled), matching push, which has no --timestamp flag;
// BuildCmd overrides Timestamp and SkipSchemaValidation from its own flags.
func (b *BuildFlags) Options() cli.BuildFlagsOptions {
	return cli.BuildFlagsOptions{
		NoCache:          b.NoCache,
		SeparateWeights:  b.SeparateWeights,
		Secrets:          b.Secrets,
		ProgressOutput:   b.Progress,
		UseCudaBaseImage: b.UseCudaBaseImage,
		UseCogBaseImage:  b.UseCogBaseImage,
		OpenAPISchema:    b.OpenAPISchema,
		DockerfileFile:   b.Dockerfile,
		Strip:            b.Strip,
		Precompile:       b.Precompile,
		Timestamp:        -1,
	}
}

// BuildOptions constructs a model.BuildOptions from the current flag values.
// The imageName and annotations parameters vary by caller (build vs push).
func (b *BuildFlags) BuildOptions(imageName string, annotations map[string]string) model.BuildOptions {
	return b.Options().ModelBuildOptions(imageName, annotations)
}

// ValidateMutualExclusivity ensures that at most one of --use-cog-base-image,
// --use-cuda-base-image, and --dockerfile is explicitly set.
func (b *BuildFlags) ValidateMutualExclusivity() error {
	var flagsSet []string
	if b.UseCogBaseImage != nil {
		flagsSet = append(flagsSet, "--use-cog-base-image")
	}
	if b.UseCudaBaseImage != "auto" {
		flagsSet = append(flagsSet, "--use-cuda-base-image")
	}
	if b.Dockerfile != "" {
		flagsSet = append(flagsSet, "--dockerfile")
	}
	if len(flagsSet) > 1 {
		return fmt.Errorf("The flags %s are mutually exclusive: you can only set one of them", strings.Join(flagsSet, " and "))
	}
	return nil
}

// progressDefault returns the default progress output based on environment.
func progressDefault() string {
	if v := os.Getenv("BUILDKIT_PROGRESS"); v != "" {
		return v
	}
	if os.Getenv("TERM") == "dumb" {
		return "plain"
	}
	return "auto"
}
