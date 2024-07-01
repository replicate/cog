package image

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/dockerfile"
	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/pkg/weights"
)

const dockerignoreBackupPath = ".dockerignore.cog.bak"
const weightsManifestPath = ".cog/cache/weights_manifest.json"
const bundledSchemaFile = ".cog/openapi_schema.json"
const bundledSchemaPy = ".cog/schema.py"

// Build a Cog model from a config
//
// This is separated out from docker.Build(), so that can be as close as possible to the behavior of 'docker build'.
func Build(cfg *config.Config, dir, imageName string, secrets []string, noCache, separateWeights bool, useCudaBaseImage string, progressOutput string, schemaFile string, dockerfileFile string, useCogBaseImage bool) error {
	console.Infof("Building Docker image from environment in cog.yaml as %s...", imageName)

	// remove bundled schema files that may be left from previous builds
	_ = os.Remove(bundledSchemaFile)
	_ = os.Remove(bundledSchemaPy)

	var cogBaseImageName string

	if dockerfileFile != "" {
		dockerfileContents, err := os.ReadFile(dockerfileFile)
		if err != nil {
			return fmt.Errorf("Failed to read Dockerfile at %s: %w", dockerfileFile, err)
		}
		if err := docker.Build(dir, string(dockerfileContents), imageName, secrets, noCache, progressOutput, config.BuildSourceEpochTimestamp); err != nil {
			return fmt.Errorf("Failed to build Docker image: %w", err)
		}
	} else {
		generator, err := dockerfile.NewGenerator(cfg, dir)
		if err != nil {
			return fmt.Errorf("Error creating Dockerfile generator: %w", err)
		}
		defer func() {
			if err := generator.Cleanup(); err != nil {
				console.Warnf("Error cleaning up Dockerfile generator: %s", err)
			}
		}()
		generator.SetUseCudaBaseImage(useCudaBaseImage)
		generator.SetUseCogBaseImage(useCogBaseImage)

		if generator.IsUsingCogBaseImage() {
			cogBaseImageName, err = generator.BaseImage()
			if err != nil {
				return fmt.Errorf("Failed to get cog base image name: %s", err)
			}
		}

		if separateWeights {
			weightsDockerfile, runnerDockerfile, dockerignore, err := generator.GenerateModelBaseWithSeparateWeights(imageName)
			if err != nil {
				return fmt.Errorf("Failed to generate Dockerfile: %w", err)
			}

			if err := backupDockerignore(); err != nil {
				return fmt.Errorf("Failed to backup .dockerignore file: %w", err)
			}

			weightsManifest, err := generator.GenerateWeightsManifest()
			if err != nil {
				return fmt.Errorf("Failed to generate weights manifest: %w", err)
			}
			cachedManifest, _ := weights.LoadManifest(weightsManifestPath)
			changed := cachedManifest == nil || !weightsManifest.Equal(cachedManifest)
			if changed {
				if err := buildWeightsImage(dir, weightsDockerfile, imageName+"-weights", secrets, noCache, progressOutput); err != nil {
					return fmt.Errorf("Failed to build model weights Docker image: %w", err)
				}
				err := weightsManifest.Save(weightsManifestPath)
				if err != nil {
					return fmt.Errorf("Failed to save weights hash: %w", err)
				}
			} else {
				console.Info("Weights unchanged, skip rebuilding and use cached image...")
			}

			if err := buildRunnerImage(dir, runnerDockerfile, dockerignore, imageName, secrets, noCache, progressOutput); err != nil {
				return fmt.Errorf("Failed to build runner Docker image: %w", err)
			}
		} else {
			dockerfileContents, err := generator.GenerateDockerfileWithoutSeparateWeights()
			if err != nil {
				return fmt.Errorf("Failed to generate Dockerfile: %w", err)
			}
			if err := docker.Build(dir, dockerfileContents, imageName, secrets, noCache, progressOutput, config.BuildSourceEpochTimestamp); err != nil {
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
		schema, err := GenerateOpenAPISchema(imageName, cfg.Build.GPU)
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
	err := os.WriteFile(bundledSchemaFile, schemaJSON, 0o644)
	if err != nil {
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

	labels := map[string]string{
		global.LabelNamespace + "version":        global.Version,
		global.LabelNamespace + "config":         string(bytes.TrimSpace(configJSON)),
		global.LabelNamespace + "openapi_schema": string(schemaJSON),
		// Mark the image as having an appropriate init entrypoint. We can use this
		// to decide how/if to shim the image.
		global.LabelNamespace + "has_init": "true",
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

		manifest, err := img.Manifest()
		if err != nil {
			return fmt.Errorf("Failed to get manifest for cog base image: %w", err)
		}

		if len(manifest.Layers) == 0 {
			return fmt.Errorf("Cog base image has no layers: %s", cogBaseImageName)
		}

		lastLayerIndex := len(manifest.Layers) - 1
		lastLayer := manifest.Layers[lastLayerIndex].Digest.String()
		console.Debugf("Last layer of the cog base image: %s", lastLayer)

		labels[global.LabelNamespace+"cog-base-image-last-layer-sha"] = lastLayer
		labels[global.LabelNamespace+"cog-base-image-last-layer-idx"] = fmt.Sprintf("%d", lastLayerIndex)
	}

	if isGitRepo(dir) {
		if commit, err := gitHead(dir); commit != "" && err == nil {
			labels["org.opencontainers.image.revision"] = commit
		} else {
			console.Info("Unable to determine Git commit")
		}

		if tag, err := gitTag(dir); tag != "" && err == nil {
			labels["org.opencontainers.image.version"] = tag
		} else {
			console.Info("Unable to determine Git tag")
		}
	}

	if err := docker.BuildAddLabelsAndSchemaToImage(imageName, labels, bundledSchemaFile, bundledSchemaPy); err != nil {
		return fmt.Errorf("Failed to add labels to image: %w", err)
	}
	return nil
}

func BuildBase(cfg *config.Config, dir string, useCudaBaseImage string, useCogBaseImage bool, progressOutput string) (string, error) {
	// TODO: better image management so we don't eat up disk space
	// https://github.com/replicate/cog/issues/80
	imageName := config.BaseDockerImageName(dir)

	console.Info("Building Docker image from environment in cog.yaml...")
	generator, err := dockerfile.NewGenerator(cfg, dir)
	if err != nil {
		return "", fmt.Errorf("Error creating Dockerfile generator: %w", err)
	}
	defer func() {
		if err := generator.Cleanup(); err != nil {
			console.Warnf("Error cleaning up Dockerfile generator: %s", err)
		}
	}()

	generator.SetUseCudaBaseImage(useCudaBaseImage)
	generator.SetUseCogBaseImage(useCogBaseImage)

	dockerfileContents, err := generator.GenerateModelBase()
	if err != nil {
		return "", fmt.Errorf("Failed to generate Dockerfile: %w", err)
	}
	if err := docker.Build(dir, dockerfileContents, imageName, []string{}, false, progressOutput, config.BuildSourceEpochTimestamp); err != nil {
		return "", fmt.Errorf("Failed to build Docker image: %w", err)
	}
	return imageName, nil
}

func isGitRepo(dir string) bool {
	if _, err := os.Stat(path.Join(dir, ".git")); os.IsNotExist(err) {
		return false
	}

	return true
}

func gitHead(dir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}

	commit := string(bytes.TrimSpace(out))

	return commit, nil
}

func gitTag(dir string) (string, error) {
	cmd := exec.Command("git", "describe", "--tags", "--dirty")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}

	tag := string(bytes.TrimSpace(out))

	return tag, nil
}

func buildWeightsImage(dir, dockerfileContents, imageName string, secrets []string, noCache bool, progressOutput string) error {
	if err := makeDockerignoreForWeightsImage(); err != nil {
		return fmt.Errorf("Failed to create .dockerignore file: %w", err)
	}
	if err := docker.Build(dir, dockerfileContents, imageName, secrets, noCache, progressOutput, config.BuildSourceEpochTimestamp); err != nil {
		return fmt.Errorf("Failed to build Docker image for model weights: %w", err)
	}
	return nil
}

func buildRunnerImage(dir, dockerfileContents, dockerignoreContents, imageName string, secrets []string, noCache bool, progressOutput string) error {
	if err := writeDockerignore(dockerignoreContents); err != nil {
		return fmt.Errorf("Failed to write .dockerignore file with weights included: %w", err)
	}
	if err := docker.Build(dir, dockerfileContents, imageName, secrets, noCache, progressOutput, config.BuildSourceEpochTimestamp); err != nil {
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
