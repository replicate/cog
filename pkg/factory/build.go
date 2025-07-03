package factory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/dockercontext"
	"github.com/replicate/cog/pkg/dockerfile"
)

// Build builds an image using the experimental builder. It intentionally
// handles only a subset of flags – most parameters are ignored for now.
func (f *Factory) Build(ctx context.Context, cfg *config.Config, dir, imageName string, secrets []string, noCache bool, progressOutput string) error {
	spec, err := BuildSpecFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to derive build spec: %w", err)
	}

	dockerfile, err := GenerateDockerfile(spec)
	if err != nil {
		return fmt.Errorf("failed to generate Dockerfile: %w", err)
	}

	// Ensure the embedded cog wheel exists in context at .cog/<wheel>
	if err := ensureCogWheelInContext(dir, spec.CogWheelFilename); err != nil {
		return err
	}

	fmt.Println(dockerfile)

	buildOpts := command.ImageBuildOptions{
		WorkingDir:         dir,
		DockerfileContents: dockerfile,
		ImageName:          imageName,
		Secrets:            secrets,
		NoCache:            noCache,
		ProgressOutput:     progressOutput,
		Epoch:              &config.BuildSourceEpochTimestamp,
		// Standard build context – use project directory directly.
	}

	if err := f.dockerProvider.ImageBuild(ctx, buildOpts); err != nil {
		return fmt.Errorf("docker build failed: %w", err)
	}

	// Optionally export dev image (builder stage) when requested via env var
	if os.Getenv("COG_EXPORT_DEV_IMAGE") == "1" {
		devTag := imageName + "-dev"

		devDockerfile := dockerfile + "\nFROM build as dev\n"

		devOpts := command.ImageBuildOptions{
			WorkingDir:         dir,
			DockerfileContents: devDockerfile,
			ImageName:          devTag,
			Secrets:            secrets,
			NoCache:            noCache,
			ProgressOutput:     progressOutput,
			Epoch:              &config.BuildSourceEpochTimestamp,
		}

		if err := f.dockerProvider.ImageBuild(ctx, devOpts); err != nil {
			return fmt.Errorf("docker build (dev image) failed: %w", err)
		}
	}

	// TODO: schema validation, label injection, pip freeze – defer to
	// legacy code in a follow-up.

	return nil
}

// writes embedded cog wheel to .cog folder under project dir so COPY succeeds.
func ensureCogWheelInContext(projectDir, wheelFilename string) error {
	if wheelFilename == "" {
		return nil
	}
	data, _, err := dockerfile.ReadWheelFile()
	if err != nil {
		return err
	}
	cogDir, err := dockercontext.CogBuildArtifactsDirPath(projectDir)
	if err != nil {
		return err
	}
	wheelPath := filepath.Join(cogDir, wheelFilename)
	if err := os.WriteFile(wheelPath, data, 0o644); err != nil {
		return err
	}
	return nil
}
