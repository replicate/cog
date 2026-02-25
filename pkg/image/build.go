package image

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
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
	"github.com/replicate/cog/pkg/registry"
	"github.com/replicate/cog/pkg/schemagen"
	"github.com/replicate/cog/pkg/util/console"
	cogversion "github.com/replicate/cog/pkg/util/version"
	"github.com/replicate/cog/pkg/weights"
	"github.com/replicate/cog/pkg/wheels"
)

const dockerignoreBackupPath = ".dockerignore.cog.bak"
const weightsManifestPath = ".cog/cache/weights_manifest.json"
const bundledSchemaFile = ".cog/openapi_schema.json"

var errGit = errors.New("git error")

// Build a Cog model from a config and returns the image ID (sha256:...) on success.
//
// This is separated out from docker.Build(), so that can be as close as possible to the behavior of 'docker build'.
func Build(
	ctx context.Context,
	cfg *config.Config,
	dir,
	imageName string,
	configFilename string,
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
	excludeSource bool,
	skipSchemaValidation bool,
	annotations map[string]string,
	dockerCommand command.Command,
	client registry.Client) (string, error) {
	console.Infof("Building Docker image from environment in cog.yaml as %s...", imageName)

	// remove bundled schema files that may be left from previous builds
	_ = os.Remove(bundledSchemaFile)

	if err := checkCompatibleDockerIgnore(dir); err != nil {
		return "", err
	}

	// Determine whether to use the static schema generator (cog-schema-gen) or
	// fall back to the legacy runtime path (boot container + python introspection).
	//
	// Static generation is used when:
	//   - The cog-schema-gen binary is available, AND
	//   - The SDK version is >= 0.17.0 (or unpinned/latest/dev)
	//
	// Legacy generation is needed for SDK < 0.17.0 which uses pydantic-based
	// schemas that cannot be statically analyzed.
	useStatic := !skipSchemaValidation && schemaFile == "" && canUseStaticSchemaGen(cfg)

	// --- Pre-build static schema generation ---
	// When using the static path, generate schema BEFORE the Docker build so we
	// fail fast on schema errors and the schema file is in the build context.
	var schemaJSON []byte
	switch {
	case useStatic:
		console.Info("Generating model schema...")
		data, err := generateStaticSchema(ctx, cfg, dir)
		if err != nil {
			return "", fmt.Errorf("image build failed: %w", err)
		}
		schemaJSON = data
	case !skipSchemaValidation && schemaFile != "":
		console.Infof("Validating model schema from %s...", schemaFile)
		data, err := os.ReadFile(schemaFile)
		if err != nil {
			return "", fmt.Errorf("Failed to read schema file: %w", err)
		}
		schemaJSON = data
	case skipSchemaValidation:
		console.Debug("Skipping model schema validation")
	}

	// Write and validate pre-build schema (static or from file).
	if len(schemaJSON) > 0 {
		if err := writeAndValidateSchema(schemaJSON); err != nil {
			return "", err
		}
	}

	// --- Docker build ---
	var cogBaseImageName string

	tmpImageId := imageName
	isR8imImage := strings.HasPrefix(imageName, "r8.im")
	if isR8imImage {
		hash := sha256.New()
		_, err := hash.Write([]byte(imageName))
		if err != nil {
			return "", err
		}
		tmpImageId = fmt.Sprintf("cog-tmp:%s", hex.EncodeToString(hash.Sum(nil)))
	}

	if dockerfileFile != "" {
		dockerfileContents, err := os.ReadFile(dockerfileFile)
		if err != nil {
			return "", fmt.Errorf("Failed to read Dockerfile at %s: %w", dockerfileFile, err)
		}

		buildOpts := command.ImageBuildOptions{
			WorkingDir:         dir,
			DockerfileContents: string(dockerfileContents),
			ImageName:          tmpImageId,
			Secrets:            secrets,
			NoCache:            noCache,
			ProgressOutput:     progressOutput,
			Epoch:              &config.BuildSourceEpochTimestamp,
			ContextDir:         dockercontext.StandardBuildDirectory,
		}
		if _, err := dockerCommand.ImageBuild(ctx, buildOpts); err != nil {
			return "", fmt.Errorf("Failed to build Docker image: %w", err)
		}
	} else {
		generator, err := dockerfile.NewGenerator(cfg, dir, configFilename, dockerCommand, client, true)
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
		generator.SetStrip(strip)
		generator.SetPrecompile(precompile)
		generator.SetUseCudaBaseImage(useCudaBaseImage)
		if useCogBaseImage != nil {
			generator.SetUseCogBaseImage(*useCogBaseImage)
		}

		if generator.IsUsingCogBaseImage() {
			cogBaseImageName, err = generator.BaseImage(ctx)
			if err != nil {
				return "", fmt.Errorf("Failed to get cog base image name: %s", err)
			}
		}

		if separateWeights {
			weightsDockerfile, runnerDockerfile, dockerignore, err := generator.GenerateModelBaseWithSeparateWeights(ctx, imageName)
			if err != nil {
				return "", fmt.Errorf("Failed to generate Dockerfile: %w", err)
			}

			if err := backupDockerignore(); err != nil {
				return "", fmt.Errorf("Failed to backup .dockerignore file: %w", err)
			}

			weightsManifest, err := generator.GenerateWeightsManifest(ctx)
			if err != nil {
				return "", fmt.Errorf("Failed to generate weights manifest: %w", err)
			}
			cachedManifest, _ := weights.LoadManifest(weightsManifestPath)
			changed := cachedManifest == nil || !weightsManifest.Equal(cachedManifest)
			if changed {
				if err := buildWeightsImage(ctx, dockerCommand, dir, weightsDockerfile, imageName+"-weights", secrets, noCache, progressOutput, contextDir, buildContexts); err != nil {
					return "", fmt.Errorf("Failed to build model weights Docker image: %w", err)
				}
				err := weightsManifest.Save(weightsManifestPath)
				if err != nil {
					return "", fmt.Errorf("Failed to save weights hash: %w", err)
				}
			} else {
				console.Info("Weights unchanged, skip rebuilding and use cached image...")
			}

			if err := buildRunnerImage(ctx, dockerCommand, dir, runnerDockerfile, dockerignore, imageName, secrets, noCache, progressOutput, contextDir, buildContexts); err != nil {
				return "", fmt.Errorf("Failed to build runner Docker image: %w", err)
			}
		} else {
			var dockerfileContents string
			if excludeSource {
				// Dev mode (cog serve): same layers as cog build but without
				// COPY . /src — source is volume-mounted at runtime instead.
				// This shares Docker layer cache with full builds.
				dockerfileContents, err = generator.GenerateModelBase(ctx)
			} else {
				dockerfileContents, err = generator.GenerateDockerfileWithoutSeparateWeights(ctx)
			}
			if err != nil {
				return "", fmt.Errorf("Failed to generate Dockerfile: %w", err)
			}

			buildOpts := command.ImageBuildOptions{
				WorkingDir:         dir,
				DockerfileContents: dockerfileContents,
				ImageName:          tmpImageId,
				Secrets:            secrets,
				NoCache:            noCache,
				ProgressOutput:     progressOutput,
				Epoch:              &config.BuildSourceEpochTimestamp,
				ContextDir:         contextDir,
				BuildContexts:      buildContexts,
			}

			if _, err := dockerCommand.ImageBuild(ctx, buildOpts); err != nil {
				return "", fmt.Errorf("Failed to build Docker image: %w", err)
			}
		}
	}

	// --- Post-build legacy schema generation ---
	// For SDK < 0.17.0 (or when cog-schema-gen is unavailable), generate the
	// schema by running the built image with python -m cog.command.openapi_schema.
	if len(schemaJSON) == 0 && !skipSchemaValidation {
		console.Info("Validating model schema...")
		enableGPU := cfg.Build != nil && cfg.Build.GPU
		schema, err := GenerateOpenAPISchema(ctx, dockerCommand, tmpImageId, enableGPU)
		if err != nil {
			return "", fmt.Errorf("Failed to get type signature: %w", err)
		}
		data, err := json.Marshal(schema)
		if err != nil {
			return "", fmt.Errorf("Failed to convert type signature to JSON: %w", err)
		}
		schemaJSON = data

		if err := writeAndValidateSchema(schemaJSON); err != nil {
			return "", err
		}
	}

	console.Info("Adding labels to image...")

	// We used to set the cog_version and config labels in Dockerfile, because we didn't require running the
	// built image to get those. But, the escaping of JSON inside a label inside a Dockerfile was gnarly, and
	// doesn't seem to be a problem here, so do it here instead.
	configJSON, err := json.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("Failed to convert config to JSON: %w", err)
	}

	pipFreeze, err := GeneratePipFreeze(ctx, dockerCommand, tmpImageId)
	if err != nil {
		return "", fmt.Errorf("Failed to generate pip freeze from image: %w", err)
	}

	labels := map[string]string{
		command.CogVersionLabelKey:           global.Version,
		command.CogConfigLabelKey:            string(bytes.TrimSpace(configJSON)),
		command.CogOpenAPISchemaLabelKey:     string(schemaJSON),
		global.LabelNamespace + "pip_freeze": pipFreeze,
		// Mark the image as having an appropriate init entrypoint. We can use this
		// to decide how/if to shim the image.
		global.LabelNamespace + "has_init": "true",
	}

	if cogBaseImageName != "" {
		labels[global.LabelNamespace+"cog-base-image-name"] = cogBaseImageName

		ref, err := name.ParseReference(cogBaseImageName)
		if err != nil {
			return "", fmt.Errorf("Failed to parse cog base image reference: %w", err)
		}

		img, err := remote.Image(ref)
		if err != nil {
			return "", fmt.Errorf("Failed to fetch cog base image: %w", err)
		}

		layers, err := img.Layers()
		if err != nil {
			return "", fmt.Errorf("Failed to get layers for cog base image: %w", err)
		}

		if len(layers) == 0 {
			return "", fmt.Errorf("Cog base image has no layers: %s", cogBaseImageName)
		}

		lastLayerIndex := len(layers) - 1
		layerLayerDigest, err := layers[lastLayerIndex].DiffID()
		if err != nil {
			return "", fmt.Errorf("Failed to get last layer digest for cog base image: %w", err)
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

	maps.Copy(labels, annotations)

	// The final image ID comes from the label-adding step.
	// When schema validation is skipped (cog run), there is no schema file to bundle.
	schemaFileToBundle := bundledSchemaFile
	if skipSchemaValidation {
		schemaFileToBundle = ""
	}
	imageID, err := BuildAddLabelsAndSchemaToImage(ctx, dockerCommand, tmpImageId, imageName, labels, schemaFileToBundle, progressOutput)
	if err != nil {
		return "", fmt.Errorf("Failed to add labels to image: %w", err)
	}

	// We created a temp image, so delete it. Don't "-f" so it doesn't blow anything up
	if isR8imImage {
		if err = dockerCommand.RemoveImage(ctx, tmpImageId); err != nil {
			return "", err
		}
	}

	return imageID, nil
}

// BuildAddLabelsAndSchemaToImage builds a cog model with labels and schema.
// Returns the image ID (sha256:...) of the final image.
//
// The new image is based on the provided image with the labels and schema file appended to it.
// tmpName is the source image to build from, image is the final image name/tag.
func BuildAddLabelsAndSchemaToImage(ctx context.Context, dockerClient command.Command, tmpName, image string, labels map[string]string, bundledSchemaFile string, progressOutput string) (string, error) {
	var dockerfile string
	if bundledSchemaFile != "" {
		dockerfile = fmt.Sprintf("FROM %s\nCOPY %s .cog\n", tmpName, bundledSchemaFile)
	} else {
		dockerfile = fmt.Sprintf("FROM %s\n", tmpName)
	}

	buildOpts := command.ImageBuildOptions{
		DockerfileContents: dockerfile,
		ImageName:          image,
		Labels:             labels,
		ProgressOutput:     progressOutput,
	}

	imageID, err := dockerClient.ImageBuild(ctx, buildOpts)
	if err != nil {
		return "", fmt.Errorf("Failed to add labels and schema to image: %w", err)
	}
	return imageID, nil
}

// staticSchemaGenMinSDKVersion is the minimum SDK version that supports
// static schema generation. Older SDK versions use pydantic-based runtime
// introspection and must fall back to the legacy Docker-based path.
const staticSchemaGenMinSDKVersion = "0.17.0"

// canUseStaticSchemaGen returns true if we should use the static schema
// generator (cog-schema-gen) instead of the legacy runtime path.
//
// Returns false (use legacy) when:
//   - The SDK version is explicitly pinned < 0.17.0
//   - The cog-schema-gen binary cannot be found
func canUseStaticSchemaGen(cfg *config.Config) bool {
	// Check if SDK version is pinned below the minimum for static gen.
	// When unpinned (empty), assume modern — the CLI and SDK are co-released.
	sdkVersion := resolveSDKVersion(cfg)
	if sdkVersion != "" {
		base := sdkVersion
		if m := wheels.BaseVersionRe.FindString(base); m != "" {
			base = m
		}
		if ver, err := cogversion.NewVersion(base); err == nil {
			minVer := cogversion.MustVersion(staticSchemaGenMinSDKVersion)
			if !ver.GreaterOrEqual(minVer) {
				console.Infof("SDK version %s < %s, using legacy runtime schema generation", sdkVersion, staticSchemaGenMinSDKVersion)
				return false
			}
		}
		// Unparseable version — let it through to static gen
	}

	// For SDK >= 0.17.0 (or unpinned), always use static schema generation.
	// If the binary is missing, generateStaticSchema will error — we do NOT
	// fall back to legacy because cog.command was removed in 0.17.0.
	return true
}

// resolveSDKVersion determines the SDK version that will be installed in the
// container, using the same precedence as the Dockerfile generator:
//  1. COG_SDK_WHEEL env var (parse version from "pypi:X.Y.Z" or wheel filename)
//  2. build.sdk_version in cog.yaml
//  3. Auto-detect from dist/ wheel filename
//  4. Empty string (latest/unpinned)
func resolveSDKVersion(cfg *config.Config) string {
	// 1. Env var
	if envVal := os.Getenv(wheels.CogSDKWheelEnvVar); envVal != "" {
		wc := wheels.ParseWheelValue(envVal)
		if wc != nil {
			if wc.Source == wheels.WheelSourcePyPI && wc.Version != "" {
				return wc.Version
			}
			// URL or file source — can't determine version statically
			return ""
		}
	}

	// 2. cog.yaml sdk_version
	if cfg.Build != nil && cfg.Build.SDKVersion != "" {
		return cfg.Build.SDKVersion
	}

	// 3. Auto-detect from dist/ wheel filename
	if v := wheels.DetectLocalSDKVersion(); v != "" {
		return v
	}

	// 4. Empty string (latest/unpinned from PyPI)
	return ""
}

// generateStaticSchema runs cog-schema-gen to produce the OpenAPI schema.
// When both predict and train are configured, it generates both and merges them.
func generateStaticSchema(ctx context.Context, cfg *config.Config, dir string) ([]byte, error) {
	if cfg.Predict == "" && cfg.Train == "" {
		return nil, fmt.Errorf("No predict or train reference found in cog.yaml")
	}

	schema, err := schemagen.GenerateCombined(ctx, dir, cfg.Predict, cfg.Train)
	if err != nil {
		return nil, fmt.Errorf("Failed to generate schema: %w", err)
	}

	data, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("Failed to convert schema to JSON: %w", err)
	}

	return data, nil
}

// writeAndValidateSchema writes the schema JSON to the bundled schema file and
// validates it as a well-formed OpenAPI 3.0 specification.
func writeAndValidateSchema(schemaJSON []byte) error {
	// Ensure the .cog/ directory exists before writing the schema file.
	if err := os.MkdirAll(filepath.Dir(bundledSchemaFile), 0o755); err != nil {
		return fmt.Errorf("failed to create directory for %s: %w", bundledSchemaFile, err)
	}

	if err := os.WriteFile(bundledSchemaFile, schemaJSON, 0o644); err != nil {
		return fmt.Errorf("failed to store bundled schema file %s: %w", bundledSchemaFile, err)
	}

	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true
	doc, err := loader.LoadFromData(schemaJSON)
	if err != nil {
		return fmt.Errorf("Failed to load model schema JSON: %w", err)
	}
	if err := doc.Validate(loader.Context); err != nil {
		return fmt.Errorf("Model schema is invalid: %w\n\n%s", err, string(schemaJSON))
	}

	return nil
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
	if _, err := dockerClient.ImageBuild(ctx, buildOpts); err != nil {
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
	if _, err := dockerClient.ImageBuild(ctx, buildOpts); err != nil {
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
		return errors.New("The .cog tmp path cannot be ignored by docker in .dockerignore")
	}
	return nil
}
