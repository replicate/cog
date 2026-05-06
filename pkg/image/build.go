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
	"slices"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/dockerfile"
	"github.com/replicate/cog/pkg/dotcog"
	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/registry"
	"github.com/replicate/cog/pkg/schema"
	"github.com/replicate/cog/pkg/schema/python"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/pkg/util/files"
	cogversion "github.com/replicate/cog/pkg/util/version"
	weightslockfile "github.com/replicate/cog/pkg/weights/lockfile"
	"github.com/replicate/cog/pkg/weightslegacy"
	"github.com/replicate/cog/pkg/wheels"
)

// cogBuildContextName is the named build context for build staging
// artifacts (.cog/build/). Dockerfile COPY instructions reference it
// via --from=cog_build.
const cogBuildContextName = "cog_build"

// defaultExcludePatterns filters .cog/ out of the project context mount
// so weight blobs, mount dirs, and build caches are never sent to the
// Docker daemon.
var defaultExcludePatterns = []string{dotcog.Name + "/"}

var errGit = errors.New("git error")

// buildPaths holds resolved file paths within the .cog/ directory for a
// single build invocation. Avoids hardcoded path constants spread across
// helper functions.
type buildPaths struct {
	buildDir        string // .cog/build/ -- staging dir for wheels, schema, manifest
	schemaFile      string // .cog/build/openapi_schema.json
	weightsFile     string // .cog/build/weights.json
	weightsManifest string // .cog/cache/weights_manifest.json (legacy separate-weights)
	// rootSchemaFile and rootWeightsFile are copies at .cog/ root level.
	// When cog predict/train/serve volume-mounts the project dir at /src,
	// the host's .cog/ shadows the image's .cog/ -- so coglet needs these
	// files on the host filesystem, not just in the image layer.
	rootSchemaFile  string // .cog/openapi_schema.json
	rootWeightsFile string // .cog/weights.json
}

// Build a Cog model from a config and returns the image ID (sha256:...) on success.
//
// This is separated out from docker.Build(), so that can be as close as possible to the behavior of 'docker build'.
func Build(
	ctx context.Context,
	cfg *config.Config,
	dc *dotcog.Dir,
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
	skipLabels bool,
	annotations map[string]string,
	dockerCommand command.Command,
	client registry.Client) (string, error) {
	release, err := dc.Lock(ctx)
	if err != nil {
		return "", err
	}
	defer release()

	// Resolve build artifact paths from the .cog/ directory. TempPath
	// registers .cog/build/ for removal on dc.Close(), so build staging
	// artifacts don't accumulate between invocations.
	buildDir, err := dc.TempPath("build")
	if err != nil {
		return "", fmt.Errorf("create build cache dir: %w", err)
	}
	bp := buildPaths{
		buildDir:        buildDir,
		schemaFile:      filepath.Join(buildDir, "openapi_schema.json"),
		weightsFile:     filepath.Join(buildDir, "weights.json"),
		weightsManifest: filepath.Join(dc.Root(), "cache", "weights_manifest.json"),
		// Schema and weights also go under .cog/ root so they're visible
		// when the project dir is volume-mounted at /src (cog predict/train/serve).
		// The bundle step COPYs from .cog/build/ via the cog_build context,
		// but the volume mount shadows the image's /src/.cog/ with the host's.
		rootSchemaFile:  filepath.Join(dc.Root(), "openapi_schema.json"),
		rootWeightsFile: filepath.Join(dc.Root(), "weights.json"),
	}

	// Determine whether to use the static schema generator (Go tree-sitter) or
	// the legacy runtime path (boot container + python introspection).
	//
	// Static generation is the default for all commands. The legacy runtime
	// path (boot container + `python -m cog.command.openapi_schema`) is opt-in
	// via COG_LEGACY_SCHEMA=1 for users pinned to SDKs < 0.17.0 or hitting
	// static-parser edge cases that haven't been resolved yet. On static-
	// parser errors that look like incomplete type resolution rather than
	// hard user bugs (ErrUnresolvableType), `cog build` automatically falls
	// back to the runtime path — the opt-out flag is only needed when users
	// want to force the runtime path from the start.
	//
	// For SDK versions < 0.17.0 (pydantic-based schemas), the static parser
	// cannot analyze the model and we silently route to the runtime path.
	needsSchema := !skipSchemaValidation && schemaFile == ""
	useStatic := needsSchema && useStaticSchemaGen(cfg)

	// --- Pre-build static schema generation ---
	// When using the static path, generate schema BEFORE the Docker build so we
	// fail fast on schema errors and the schema file is in the build context.
	var schemaJSON []byte
	switch {
	case useStatic:
		console.Debug("Generating model schema (static)...")
		data, err := generateStaticSchema(cfg, dir)
		if err == nil {
			schemaJSON = data
			break
		}

		// For `cog build` only: fall back to the post-build legacy runtime
		// schema generation which can handle types that require Python import
		// (e.g. package __init__.py modules, pydantic v2 BaseModel subclasses).
		var se *schema.SchemaError
		if !skipLabels && errors.As(err, &se) && se.Kind == schema.ErrUnresolvableType {
			console.Warnf("Static schema generation failed: %s", err)
			console.Warn("Falling back to legacy runtime schema generation...")
			// leave schemaJSON nil — the post-build legacy path will handle it
			break
		}

		return "", fmt.Errorf("image build failed: %w", err)
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
	// Written atomically to .cog/ root (visible via volume mount for
	// cog predict/train/serve), then copied to .cog/build/ for the
	// cog_build named build context.
	if len(schemaJSON) > 0 {
		if err := writeAndValidateSchema(schemaJSON, bp.rootSchemaFile); err != nil {
			return "", err
		}
		if err := files.Copy(bp.rootSchemaFile, bp.schemaFile); err != nil {
			return "", err
		}
	}

	// --- Runtime weights manifest (/.cog/weights.json) ---
	// When managed weights are configured and a lockfile exists, project the
	// lockfile to the minimal runtime manifest (spec §3.3) and write it into
	// the build context so it ends up at /.cog/weights.json in the image.
	if len(cfg.Weights) > 0 {
		if err := writeRuntimeWeightsManifest(dir, bp.rootWeightsFile); err != nil {
			return "", err
		}
		if err := files.Copy(bp.rootWeightsFile, bp.weightsFile); err != nil {
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
			ContextDir:         ".",
			ExcludePatterns:    defaultExcludePatterns,
		}
		if _, err := dockerCommand.ImageBuild(ctx, buildOpts); err != nil {
			return "", fmt.Errorf("Failed to build Docker image: %w", err)
		}
	} else {
		generator, err := dockerfile.NewStandardGenerator(cfg, dir, bp.buildDir, configFilename, dockerCommand, client, true)
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
			weightsDockerfile, runnerDockerfile, weightsExcludePatterns, err := generator.GenerateModelBaseWithSeparateWeights(ctx, imageName)
			if err != nil {
				return "", fmt.Errorf("Failed to generate Dockerfile: %w", err)
			}

			weightsManifest, err := generator.GenerateWeightsManifest(ctx)
			if err != nil {
				return "", fmt.Errorf("Failed to generate weights manifest: %w", err)
			}
			cachedManifest, _ := weightslegacy.LoadManifest(bp.weightsManifest)
			changed := cachedManifest == nil || !weightsManifest.Equal(cachedManifest)
			if changed {
				if err := buildContextImage(ctx, dockerCommand, dir, weightsDockerfile, imageName+"-weights", secrets, noCache, progressOutput, contextDir, buildContexts, defaultExcludePatterns); err != nil {
					return "", fmt.Errorf("Failed to build model weights Docker image: %w", err)
				}
				err := weightsManifest.Save(bp.weightsManifest)
				if err != nil {
					return "", fmt.Errorf("Failed to save weights hash: %w", err)
				}
			} else {
				console.Info("Weights unchanged, skip rebuilding and use cached image...")
			}

			// Exclude weight dirs/files from the runner context so COPY . /src
			// doesn't duplicate them (they arrive via COPY --from=weights).
			runnerExclude := slices.Concat(defaultExcludePatterns, weightsExcludePatterns)
			if err := buildContextImage(ctx, dockerCommand, dir, runnerDockerfile, tmpImageId, secrets, noCache, progressOutput, contextDir, buildContexts, runnerExclude); err != nil {
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
				ExcludePatterns:    defaultExcludePatterns,
				BuildCacheDir:      generator.BuildCacheDir(),
			}

			if _, err := dockerCommand.ImageBuild(ctx, buildOpts); err != nil {
				return "", fmt.Errorf("Failed to build Docker image: %w", err)
			}
		}
	}

	// --- Post-build legacy schema generation ---
	// For SDK < 0.17.0 (or when static gen was not used), generate the schema
	// by running the built image with python -m cog.command.openapi_schema.
	// This must run before the skipLabels early return so that cog train/predict/serve
	// have a schema available for input validation and -i flag parsing.
	if len(schemaJSON) == 0 && !skipSchemaValidation {
		console.Info("Validating model schema...")
		enableGPU := cfg.Build != nil && cfg.Build.GPU
		// When excludeSource is true (cog serve/predict/train), /src was not
		// COPYed into the image, so volume-mount the project directory.
		sourceDir := ""
		if excludeSource {
			sourceDir = dir
		}
		legacySchema, err := GenerateOpenAPISchema(ctx, dockerCommand, tmpImageId, enableGPU, sourceDir)
		if err != nil {
			return "", fmt.Errorf("Failed to get type signature: %w", err)
		}
		data, err := json.Marshal(legacySchema)
		if err != nil {
			return "", fmt.Errorf("Failed to convert type signature to JSON: %w", err)
		}
		schemaJSON = data

		if err := writeAndValidateSchema(schemaJSON, bp.rootSchemaFile); err != nil {
			return "", err
		}
		if err := files.Copy(bp.rootSchemaFile, bp.schemaFile); err != nil {
			return "", err
		}
	}

	bundleFiles := collectBundleFiles(schemaJSON, &bp)

	// When skipLabels is true (cog exec/predict/serve/train), skip the expensive
	// label-adding phase. This image is for local use only and won't be distributed,
	// so we don't need metadata labels, pip freeze, or git info.
	// We still need the schema bundled, so do a minimal second build to add it.
	if skipLabels {
		if len(bundleFiles) > 0 {
			buildOpts := command.ImageBuildOptions{
				DockerfileContents: bundleDockerfile(tmpImageId, bundleFiles),
				ImageName:          tmpImageId,
				ProgressOutput:     progressOutput,
				BuildCacheDir:      bp.buildDir,
				BuildContexts: map[string]string{
					cogBuildContextName: bp.buildDir,
				},
			}
			if _, err := dockerCommand.ImageBuild(ctx, buildOpts); err != nil {
				return "", fmt.Errorf("Failed to bundle .cog files into image: %w", err)
			}
		}
		return tmpImageId, nil
	}

	console.Info("Adding labels to image...")
	console.Info("")

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

		// name.Insecure allows HTTP fallback for local/test registries,
		// consistent with ParseReference calls in pkg/registry/.
		ref, err := name.ParseReference(cogBaseImageName, name.Insecure)
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
		console.Debug("Unable to determine Git commit")
	}

	if tag, err := gitTag(ctx, dir); tag != "" && err == nil {
		labels["org.opencontainers.image.version"] = tag
	} else {
		console.Debug("Unable to determine Git tag")
	}

	maps.Copy(labels, annotations)

	// The final image ID comes from the label-adding step.
	imageID, err := BuildAddLabelsAndSchemaToImage(ctx, dockerCommand, tmpImageId, imageName, labels, bundleFiles, progressOutput, bp.buildDir)
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

// BuildAddLabelsAndSchemaToImage builds a cog model with labels and bundled
// .cog/ files. Returns the image ID (sha256:...) of the final image.
//
// The new image is based on the provided image with the labels and any
// bundled files (schema, weights manifest, etc.) appended to it.
// tmpName is the source image to build from, image is the final image name/tag.
func BuildAddLabelsAndSchemaToImage(ctx context.Context, dockerClient command.Command, tmpName, image string, labels map[string]string, bundleFiles []string, progressOutput string, buildCacheDir string) (string, error) {
	buildOpts := command.ImageBuildOptions{
		DockerfileContents: bundleDockerfile(tmpName, bundleFiles),
		ImageName:          image,
		Labels:             labels,
		ProgressOutput:     progressOutput,
		BuildCacheDir:      buildCacheDir,
		BuildContexts: map[string]string{
			cogBuildContextName: buildCacheDir,
		},
	}

	imageID, err := dockerClient.ImageBuild(ctx, buildOpts)
	if err != nil {
		return "", fmt.Errorf("Failed to add labels to image: %w", err)
	}
	return imageID, nil
}

// staticSchemaGenMinSDKVersion is the minimum SDK version that supports
// static schema generation. Older SDK versions use pydantic-based runtime
// introspection and must fall back to the legacy Docker-based path.
const staticSchemaGenMinSDKVersion = "0.17.0"

// legacySchemaEnvVar is the opt-out toggle: setting it to a truthy value
// forces the legacy runtime schema path instead of the default static path.
// Kept as a lifeline for users pinned to old SDKs or hitting static-parser
// bugs that haven't been resolved yet.
const legacySchemaEnvVar = "COG_LEGACY_SCHEMA"

// useStaticSchemaGen returns true when the static schema generator should
// run. Static generation is the default; the user can force the legacy
// runtime path by setting COG_LEGACY_SCHEMA=1 (or "true"). The static path
// is also bypassed when the configured SDK version is explicitly pinned
// below staticSchemaGenMinSDKVersion (older SDKs use pydantic-based
// schemas the static parser cannot analyze).
func useStaticSchemaGen(cfg *config.Config) bool {
	if isTruthyEnv(legacySchemaEnvVar) {
		return false
	}

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
	}
	return true
}

// isTruthyEnv reports whether the named env var is set to a truthy value
// ("1" or "true", case-insensitive). Empty and unset are both false.
func isTruthyEnv(name string) bool {
	v := strings.ToLower(os.Getenv(name))
	return v == "1" || v == "true"
}

// resolveSDKVersion determines the SDK version that will be installed in the
// container, using the same precedence as the Dockerfile generator:
//  1. COG_SDK_WHEEL env var (parse version from "pypi:X.Y.Z")
//  2. build.sdk_version in cog.yaml
//  3. Auto-detect from dist/ wheel filename
//  4. Empty string (latest/unpinned)
func resolveSDKVersion(cfg *config.Config) string {
	if envVal := os.Getenv(wheels.CogSDKWheelEnvVar); envVal != "" {
		wc := wheels.ParseWheelValue(envVal)
		if wc != nil && wc.Source == wheels.WheelSourcePyPI && wc.Version != "" {
			return wc.Version
		}
		return ""
	}
	if cfg.Build != nil && cfg.Build.SDKVersion != "" {
		if cfg.Build.SDKVersion == wheels.PreReleaseSentinel {
			return "" // unpinned; latest pre-release resolved at build time
		}
		return cfg.Build.SDKVersion
	}
	if v := wheels.DetectLocalSDKVersion(); v != "" {
		return v
	}
	return ""
}

// generateStaticSchema runs the Go tree-sitter parser to produce the OpenAPI schema.
// When both predict and train are configured, it generates both and merges them.
func generateStaticSchema(cfg *config.Config, dir string) ([]byte, error) {
	if cfg.Predict == "" && cfg.Train == "" {
		return nil, fmt.Errorf("no predict or train reference found in cog.yaml")
	}
	return schema.GenerateCombined(dir, cfg.Predict, cfg.Train, python.ParsePredictor)

}

// writeAndValidateSchema validates the schema JSON as a well-formed OpenAPI 3.0
// specification, then atomically writes it to schemaPath (write-to-temp + rename).
func writeAndValidateSchema(schemaJSON []byte, schemaPath string) error {
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true
	doc, err := loader.LoadFromData(schemaJSON)
	if err != nil {
		return fmt.Errorf("Failed to load model schema JSON: %w", err)
	}
	if err := doc.Validate(loader.Context); err != nil {
		return fmt.Errorf("Model schema is invalid: %w\n\n%s", err, string(schemaJSON))
	}
	return files.AtomicWrite(schemaPath, schemaJSON)
}

// writeRuntimeWeightsManifest projects the lockfile to /.cog/weights.json (spec §3.3).
func writeRuntimeWeightsManifest(dir string, weightsPath string) error {
	lockPath := filepath.Join(dir, weightslockfile.WeightsLockFilename)
	lock, err := weightslockfile.LoadWeightsLock(lockPath)
	if err != nil {
		return fmt.Errorf("managed weights configured but no lockfile found: %w\nRun 'cog weights import' first", err)
	}

	manifest := lock.RuntimeManifest()
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize runtime weights manifest: %w", err)
	}

	if err := files.AtomicWrite(weightsPath, data); err != nil {
		return fmt.Errorf("write runtime weights manifest: %w", err)
	}
	console.Debugf("Wrote runtime weights manifest to %s (%d weights)", weightsPath, len(manifest.Weights))
	return nil
}

// collectBundleFiles returns the list of .cog/build/ files that should be
// COPYed into the final image layer. It checks schemaJSON (non-nil = schema
// was generated) and probes the filesystem for the weights manifest.
func collectBundleFiles(schemaJSON []byte, bp *buildPaths) []string {
	var files []string
	if len(schemaJSON) > 0 {
		files = append(files, bp.schemaFile)
	}
	if _, err := os.Stat(bp.weightsFile); err == nil {
		files = append(files, bp.weightsFile)
	}
	return files
}

// bundleDockerfile returns a Dockerfile that COPYs build artifacts into
// the image via the cog_build named build context. Files are referenced
// by basename since cog_build is rooted at .cog/build/.
func bundleDockerfile(baseImage string, files []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "FROM %s\n", baseImage)
	for _, f := range files {
		fmt.Fprintf(&b, "COPY --from=%s %s %s/\n", cogBuildContextName, filepath.Base(f), dotcog.Name)
	}
	return b.String()
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

func buildContextImage(ctx context.Context, dockerClient command.Command, dir, dockerfileContents, imageName string, secrets []string, noCache bool, progressOutput string, contextDir string, buildContexts map[string]string, excludePatterns []string) error {
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
		ExcludePatterns:    excludePatterns,
	}
	if _, err := dockerClient.ImageBuild(ctx, buildOpts); err != nil {
		return fmt.Errorf("Failed to build Docker image: %w", err)
	}
	return nil
}
