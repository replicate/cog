package image

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/dockercontext"
	"github.com/replicate/cog/pkg/dockerfile"
	"github.com/replicate/cog/pkg/dockerignore"
	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/http"
	"github.com/replicate/cog/pkg/procedure"
	"github.com/replicate/cog/pkg/registry"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/pkg/weights"
)

const dockerignoreBackupPath = ".dockerignore.cog.bak"
const weightsManifestPath = ".cog/cache/weights_manifest.json"
const bundledSchemaFile = ".cog/openapi_schema.json"

var errGit = errors.New("git error")

// Build a Cog model from a config
//
// This is separated out from docker.Build(), so that can be as close as possible to the behavior of 'docker build'.
func Build(
	ctx context.Context,
	cfg *config.Config,
	dir,
	imageName string,
	secrets []string,
	noCache,
	separateWeights bool,
	useCudaBaseImage string,
	progressOutput string,
	schemaFile string,
	dockerfileFile string,
	useCogBaseImage *bool,
	strip bool,
	precompile bool,
	fastFlag bool,
	annotations map[string]string,
	localImage bool,
	dockerCommand command.Command,
	client registry.Client,
	pipelinesImage bool) error {
	console.Infof("Building Docker image from environment in cog.yaml as %s...", imageName)
	if fastFlag {
		console.Info("Fast build enabled.")
	}

	if pipelinesImage {
		httpClient, err := http.ProvideHTTPClient(ctx, dockerCommand)
		if err != nil {
			return err
		}
		err = procedure.Validate(dir, httpClient, cfg, true)
		if err != nil {
			return err
		}
	}

	// remove bundled schema files that may be left from previous builds
	_ = os.Remove(bundledSchemaFile)

	if err := checkCompatibleDockerIgnore(dir); err != nil {
		return err
	}

	var cogBaseImageName string

	if dockerfileFile != "" {
		dockerfileContents, err := os.ReadFile(dockerfileFile)
		if err != nil {
			return fmt.Errorf("Failed to read Dockerfile at %s: %w", dockerfileFile, err)
		}

		buildOpts := command.ImageBuildOptions{
			WorkingDir:         dir,
			DockerfileContents: string(dockerfileContents),
			ImageName:          imageName,
			Secrets:            secrets,
			NoCache:            noCache,
			ProgressOutput:     progressOutput,
			Epoch:              &config.BuildSourceEpochTimestamp,
			ContextDir:         dockercontext.StandardBuildDirectory,
		}
		if err := dockerCommand.ImageBuild(ctx, buildOpts); err != nil {
			return fmt.Errorf("Failed to build Docker image: %w", err)
		}
	} else {
		generator, err := dockerfile.NewGenerator(cfg, dir, fastFlag, dockerCommand, localImage, client, true)
		if err != nil {
			return fmt.Errorf("Error creating Dockerfile generator: %w", err)
		}
		contextDir, err := generator.BuildDir()
		if err != nil {
			return err
		}
		buildContexts, err := generator.BuildContexts()
		if err != nil {
			return err
		}
		defer func() {
			if err := generator.Cleanup(); err != nil {
				console.Warnf("Error cleaning up Dockerfile generator: %s", err)
			}
		}()
		generator.SetStrip(strip)
		generator.SetPrecompile(precompile)
		generator.SetUseCudaBaseImage(useCudaBaseImage)
		if useCogBaseImage != nil {
			generator.SetUseCogBaseImage(*useCogBaseImage)
		}

		if generator.IsUsingCogBaseImage() {
			cogBaseImageName, err = generator.BaseImage(ctx)
			if err != nil {
				return fmt.Errorf("Failed to get cog base image name: %s", err)
			}
		}

		if separateWeights {
			weightsDockerfile, runnerDockerfile, dockerignore, err := generator.GenerateModelBaseWithSeparateWeights(ctx, imageName)
			if err != nil {
				return fmt.Errorf("Failed to generate Dockerfile: %w", err)
			}

			if err := backupDockerignore(); err != nil {
				return fmt.Errorf("Failed to backup .dockerignore file: %w", err)
			}

			weightsManifest, err := generator.GenerateWeightsManifest(ctx)
			if err != nil {
				return fmt.Errorf("Failed to generate weights manifest: %w", err)
			}
			cachedManifest, _ := weights.LoadManifest(weightsManifestPath)
			changed := cachedManifest == nil || !weightsManifest.Equal(cachedManifest)
			if changed {
				if err := buildWeightsImage(ctx, dockerCommand, dir, weightsDockerfile, imageName+"-weights", secrets, noCache, progressOutput, contextDir, buildContexts); err != nil {
					return fmt.Errorf("Failed to build model weights Docker image: %w", err)
				}
				err := weightsManifest.Save(weightsManifestPath)
				if err != nil {
					return fmt.Errorf("Failed to save weights hash: %w", err)
				}
			} else {
				console.Info("Weights unchanged, skip rebuilding and use cached image...")
			}

			if err := buildRunnerImage(ctx, dockerCommand, dir, runnerDockerfile, dockerignore, imageName, secrets, noCache, progressOutput, contextDir, buildContexts); err != nil {
				return fmt.Errorf("Failed to build runner Docker image: %w", err)
			}
		} else {
			dockerfileContents, err := generator.GenerateDockerfileWithoutSeparateWeights(ctx)
			if err != nil {
				return fmt.Errorf("Failed to generate Dockerfile: %w", err)
			}

			buildOpts := command.ImageBuildOptions{
				WorkingDir:         dir,
				DockerfileContents: dockerfileContents,
				ImageName:          imageName,
				Secrets:            secrets,
				NoCache:            noCache,
				ProgressOutput:     progressOutput,
				Epoch:              &config.BuildSourceEpochTimestamp,
				ContextDir:         contextDir,
				BuildContexts:      buildContexts,
			}

			if err := dockerCommand.ImageBuild(ctx, buildOpts); err != nil {
				return fmt.Errorf("Failed to build Docker image: %w", err)
			}
		}
	}

	var schemaJSON []byte
	if schemaFile != "" {
		console.Infof("Validating model schema from %s...", schemaFile)
		data, err := os.ReadFile(schemaFile)
		if err != nil {
			return fmt.Errorf("Failed to read schema file: %w", err)
		}

		schemaJSON = data
	} else {
		console.Info("Validating model schema...")
		schema, err := GenerateOpenAPISchema(ctx, dockerCommand, imageName, cfg.Build.GPU)
		if err != nil {
			return fmt.Errorf("Failed to get type signature: %w", err)
		}

		data, err := json.Marshal(schema)
		if err != nil {
			return fmt.Errorf("Failed to convert type signature to JSON: %w", err)
		}

		schemaJSON = data
	}

	// save open_api schema file
	if err := os.WriteFile(bundledSchemaFile, schemaJSON, 0o644); err != nil {
		return fmt.Errorf("failed to store bundled schema file %s: %w", bundledSchemaFile, err)
	}

	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true
	doc, err := loader.LoadFromData(schemaJSON)
	if err != nil {
		return fmt.Errorf("Failed to load model schema JSON: %w", err)
	}
	err = doc.Validate(loader.Context)
	if err != nil {
		return fmt.Errorf("Model schema is invalid: %w\n\n%s", err, string(schemaJSON))
	}

	console.Info("Adding labels to image...")

	// We used to set the cog_version and config labels in Dockerfile, because we didn't require running the
	// built image to get those. But, the escaping of JSON inside a label inside a Dockerfile was gnarly, and
	// doesn't seem to be a problem here, so do it here instead.
	configJSON, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("Failed to convert config to JSON: %w", err)
	}

	pipFreeze, err := GeneratePipFreeze(ctx, dockerCommand, imageName, fastFlag)
	if err != nil {
		return fmt.Errorf("Failed to generate pip freeze from image: %w", err)
	}

	modelDependencies, err := GenerateModelDependencies(ctx, dockerCommand, imageName, cfg)
	if err != nil {
		return fmt.Errorf("Failed to generate model dependencies from image: %w", err)
	}

	labels := map[string]string{
		command.CogVersionLabelKey:           global.Version,
		command.CogConfigLabelKey:            string(bytes.TrimSpace(configJSON)),
		command.CogOpenAPISchemaLabelKey:     string(schemaJSON),
		global.LabelNamespace + "pip_freeze": pipFreeze,
		// Mark the image as having an appropriate init entrypoint. We can use this
		// to decide how/if to shim the image.
		global.LabelNamespace + "has_init":   "true",
		command.CogModelDependenciesLabelKey: modelDependencies,
	}

	if cogBaseImageName != "" {
		labels[global.LabelNamespace+"cog-base-image-name"] = cogBaseImageName

		ref, err := name.ParseReference(cogBaseImageName)
		if err != nil {
			return fmt.Errorf("Failed to parse cog base image reference: %w", err)
		}

		img, err := remote.Image(ref)
		if err != nil {
			return fmt.Errorf("Failed to fetch cog base image: %w", err)
		}

		layers, err := img.Layers()
		if err != nil {
			return fmt.Errorf("Failed to get layers for cog base image: %w", err)
		}

		if len(layers) == 0 {
			return fmt.Errorf("Cog base image has no layers: %s", cogBaseImageName)
		}

		lastLayerIndex := len(layers) - 1
		layerLayerDigest, err := layers[lastLayerIndex].DiffID()
		if err != nil {
			return fmt.Errorf("Failed to get last layer digest for cog base image: %w", err)
		}

		lastLayer := layerLayerDigest.String()
		console.Debugf("Last layer of the cog base image: %s", lastLayer)

		labels[global.LabelNamespace+"cog-base-image-last-layer-sha"] = lastLayer
		labels[global.LabelNamespace+"cog-base-image-last-layer-idx"] = fmt.Sprintf("%d", lastLayerIndex)
	}

	if commit, err := gitHead(ctx, dir); commit != "" && err == nil {
		labels["org.opencontainers.image.revision"] = commit
	} else {
		console.Info("Unable to determine Git commit")
	}

	if tag, err := gitTag(ctx, dir); tag != "" && err == nil {
		labels["org.opencontainers.image.version"] = tag
	} else {
		console.Info("Unable to determine Git tag")
	}

	for key, val := range annotations {
		labels[key] = val
	}

	if err := BuildAddLabelsAndSchemaToImage(ctx, dockerCommand, imageName, labels, bundledSchemaFile, progressOutput); err != nil {
		return fmt.Errorf("Failed to add labels to image: %w", err)
	}
	return nil
}

// BuildAddLabelsAndSchemaToImage builds a cog model with labels and schema.
//
// The new image is based on the provided image with the labels and schema file appended to it.
func BuildAddLabelsAndSchemaToImage(ctx context.Context, dockerClient command.Command, image string, labels map[string]string, bundledSchemaFile string, progressOutput string) error {
	dockerfile := "FROM " + image + "\n"
	dockerfile += "COPY " + bundledSchemaFile + " .cog\n"

	buildOpts := command.ImageBuildOptions{
		DockerfileContents: dockerfile,
		ImageName:          image,
		Labels:             labels,
		ProgressOutput:     progressOutput,
	}

	if err := dockerClient.ImageBuild(ctx, buildOpts); err != nil {
		return fmt.Errorf("Failed to add labels and schema to image: %w", err)
	}
	return nil
}

func BuildBase(ctx context.Context, dockerClient command.Command, cfg *config.Config, dir string, useCudaBaseImage string, useCogBaseImage *bool, progressOutput string, client registry.Client, requiresCog bool) (string, error) {
	// TODO: better image management so we don't eat up disk space
	// https://github.com/replicate/cog/issues/80
	imageName := config.BaseDockerImageName(dir)

	console.Info("Building Docker image from environment in cog.yaml...")
	generator, err := dockerfile.NewGenerator(cfg, dir, false, dockerClient, false, client, requiresCog)
	if err != nil {
		return "", fmt.Errorf("Error creating Dockerfile generator: %w", err)
	}
	contextDir, err := generator.BuildDir()
	if err != nil {
		return "", err
	}
	buildContexts, err := generator.BuildContexts()
	if err != nil {
		return "", err
	}
	defer func() {
		if err := generator.Cleanup(); err != nil {
			console.Warnf("Error cleaning up Dockerfile generator: %s", err)
		}
	}()

	generator.SetUseCudaBaseImage(useCudaBaseImage)
	if useCogBaseImage != nil {
		generator.SetUseCogBaseImage(*useCogBaseImage)
	}

	dockerfileContents, err := generator.GenerateModelBase(ctx)
	if err != nil {
		return "", fmt.Errorf("Failed to generate Dockerfile: %w", err)
	}

	buildOpts := command.ImageBuildOptions{
		WorkingDir:         dir,
		DockerfileContents: dockerfileContents,
		ImageName:          imageName,
		NoCache:            false,
		ProgressOutput:     progressOutput,
		Epoch:              &config.BuildSourceEpochTimestamp,
		ContextDir:         contextDir,
		BuildContexts:      buildContexts,
	}
	if err := dockerClient.ImageBuild(ctx, buildOpts); err != nil {
		return "", fmt.Errorf("Failed to build Docker image: %w", err)
	}
	return imageName, nil
}

func isGitWorkTree(ctx context.Context, dir string) bool {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--is-inside-work-tree").Output()
	if err != nil {
		return false
	}

	return strings.TrimSpace(string(out)) == "true"
}

func gitHead(ctx context.Context, dir string) (string, error) {
	if v, ok := os.LookupEnv("GITHUB_SHA"); ok && v != "" {
		return v, nil
	}

	if isGitWorkTree(ctx, dir) {
		ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()

		out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "HEAD").Output()
		if err != nil {
			return "", err
		}

		return string(bytes.TrimSpace(out)), nil
	}

	return "", fmt.Errorf("Failed to find HEAD commit: %w", errGit)
}

func gitTag(ctx context.Context, dir string) (string, error) {
	if v, ok := os.LookupEnv("GITHUB_REF_NAME"); ok && v != "" {
		return v, nil
	}

	if isGitWorkTree(ctx, dir) {
		ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()

		out, err := exec.CommandContext(ctx, "git", "-C", dir, "describe", "--tags", "--dirty").Output()
		if err != nil {
			return "", err
		}

		return string(bytes.TrimSpace(out)), nil
	}

	return "", fmt.Errorf("Failed to find ref name: %w", errGit)
}

func buildWeightsImage(ctx context.Context, dockerClient command.Command, dir, dockerfileContents, imageName string, secrets []string, noCache bool, progressOutput string, contextDir string, buildContexts map[string]string) error {
	if err := makeDockerignoreForWeightsImage(); err != nil {
		return fmt.Errorf("Failed to create .dockerignore file: %w", err)
	}
	buildOpts := command.ImageBuildOptions{
		WorkingDir:         dir,
		DockerfileContents: dockerfileContents,
		ImageName:          imageName,
		Secrets:            secrets,
		NoCache:            noCache,
		ProgressOutput:     progressOutput,
		Epoch:              &config.BuildSourceEpochTimestamp,
		ContextDir:         contextDir,
		BuildContexts:      buildContexts,
	}
	if err := dockerClient.ImageBuild(ctx, buildOpts); err != nil {
		return fmt.Errorf("Failed to build Docker image for model weights: %w", err)
	}
	return nil
}

func buildRunnerImage(ctx context.Context, dockerClient command.Command, dir, dockerfileContents, dockerignoreContents, imageName string, secrets []string, noCache bool, progressOutput string, contextDir string, buildContexts map[string]string) error {
	if err := writeDockerignore(dockerignoreContents); err != nil {
		return fmt.Errorf("Failed to write .dockerignore file with weights included: %w", err)
	}
	buildOpts := command.ImageBuildOptions{
		WorkingDir:         dir,
		DockerfileContents: dockerfileContents,
		ImageName:          imageName,
		Secrets:            secrets,
		NoCache:            noCache,
		ProgressOutput:     progressOutput,
		Epoch:              &config.BuildSourceEpochTimestamp,
		ContextDir:         contextDir,
		BuildContexts:      buildContexts,
	}
	if err := dockerClient.ImageBuild(ctx, buildOpts); err != nil {
		return fmt.Errorf("Failed to build Docker image: %w", err)
	}
	if err := restoreDockerignore(); err != nil {
		return fmt.Errorf("Failed to restore backup .dockerignore file: %w", err)
	}
	return nil
}

func makeDockerignoreForWeightsImage() error {
	if err := backupDockerignore(); err != nil {
		return fmt.Errorf("Failed to backup .dockerignore file: %w", err)
	}

	if err := writeDockerignore(dockerfile.DockerignoreHeader); err != nil {
		return fmt.Errorf("Failed to write .dockerignore file: %w", err)
	}
	return nil
}

func writeDockerignore(contents string) error {
	// read existing file contents from .dockerignore.cog.bak if it exists, and append to the new contents
	if _, err := os.Stat(dockerignoreBackupPath); err == nil {
		existingContents, err := os.ReadFile(dockerignoreBackupPath)
		if err != nil {
			return err
		}
		contents = string(existingContents) + "\n" + contents
	}

	return os.WriteFile(".dockerignore", []byte(contents), 0o644)
}

func backupDockerignore() error {
	if _, err := os.Stat(".dockerignore"); err != nil {
		if os.IsNotExist(err) {
			// .dockerignore file does not exist, nothing to backup
			return nil
		}
		return err
	}

	// rename the .dockerignore file to a new name
	return os.Rename(".dockerignore", dockerignoreBackupPath)
}

func restoreDockerignore() error {
	if err := os.Remove(".dockerignore"); err != nil {
		return err
	}

	if _, err := os.Stat(dockerignoreBackupPath); err != nil {
		if os.IsNotExist(err) {
			// .dockerignore backup file does not exist, nothing to restore
			return nil
		}
		return err
	}

	return os.Rename(dockerignoreBackupPath, ".dockerignore")
}

func checkCompatibleDockerIgnore(dir string) error {
	matcher, err := dockerignore.CreateMatcher(dir)
	if err != nil {
		return err
	}
	// If the matcher is nil and we don't have an error, we don't have a .dockerignore to scan.
	if matcher == nil {
		return nil
	}
	if matcher.MatchesPath(".cog") {
		return errors.New("The .cog tmp path cannot be ignored by docker in .dockerignore.")
	}
	return nil
}
