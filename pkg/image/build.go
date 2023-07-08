package image

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/dockerfile"
	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/util/console"
)

const dockerignoreBackupPath = ".dockerignore.cog.bak"

// Build a Cog model from a config
//
// This is separated out from docker.Build(), so that can be as close as possible to the behavior of 'docker build'.
func Build(cfg *config.Config, dir, imageName string, secrets []string, noCache, separateWeights bool, progressOutput string) error {
	console.Infof("Building Docker image from environment in cog.yaml as %s...", imageName)

	generator, err := dockerfile.NewGenerator(cfg, dir)
	if err != nil {
		return fmt.Errorf("Error creating Dockerfile generator: %w", err)
	}
	defer func() {
		if err := generator.Cleanup(); err != nil {
			console.Warnf("Error cleaning up Dockerfile generator: %s", err)
		}
	}()

	if separateWeights {
		weightsDockerfile, runnerDockerfile, dockerignore, err := generator.Generate(imageName)
		if err != nil {
			return fmt.Errorf("Failed to generate Dockerfile: %w", err)
		}

		if err := buildWeightsImage(dir, weightsDockerfile, imageName+"-weights", secrets, noCache, progressOutput); err != nil {
			return fmt.Errorf("Failed to build model weights Docker image: %w", err)
		}

		if err := buildRunnerImage(dir, runnerDockerfile, dockerignore, imageName, secrets, noCache, progressOutput); err != nil {
			return fmt.Errorf("Failed to build runner Docker image: %w", err)
		}
	} else {
		dockerfileContents, err := generator.GenerateDockerfileWithoutSeparateWeights()
		if err != nil {
			return fmt.Errorf("Failed to generate Dockerfile: %w", err)
		}
		if err := docker.Build(dir, dockerfileContents, imageName, secrets, noCache, progressOutput); err != nil {
			return fmt.Errorf("Failed to build Docker image: %w", err)
		}
	}

	console.Info("Adding labels to image...")
	schema, err := GenerateOpenAPISchema(imageName, cfg.Build.GPU)
	if err != nil {
		return fmt.Errorf("Failed to get type signature: %w", err)
	}
	configJSON, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("Failed to convert config to JSON: %w", err)
	}
	// We used to set the cog_version and config labels in Dockerfile, because we didn't require running the
	// built image to get those. But, the escaping of JSON inside a label inside a Dockerfile was gnarly, and
	// doesn't seem to be a problem here, so do it here instead.
	labels := map[string]string{
		global.LabelNamespace + "version": global.Version,
		global.LabelNamespace + "config":  string(bytes.TrimSpace(configJSON)),
		// Mark the image as having an appropriate init entrypoint. We can use this
		// to decide how/if to shim the image.
		global.LabelNamespace + "has_init": "true",
		// Backwards compatibility. Remove for 1.0.
		"org.cogmodel.deprecated":  "The org.cogmodel labels are deprecated. Use run.cog.",
		"org.cogmodel.cog_version": global.Version,
		"org.cogmodel.config":      string(bytes.TrimSpace(configJSON)),
	}

	// OpenAPI schema is not set if there is no predictor.
	if len((*schema).(map[string]interface{})) != 0 {
		schemaJSON, err := json.Marshal(schema)
		if err != nil {
			return fmt.Errorf("Failed to convert type signature to JSON: %w", err)
		}
		labels[global.LabelNamespace+"openapi_schema"] = string(schemaJSON)
		labels["org.cogmodel.openapi_schema"] = string(schemaJSON)
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

	if err := docker.BuildAddLabelsToImage(imageName, labels); err != nil {
		return fmt.Errorf("Failed to add labels to image: %w", err)
	}
	return nil
}

func BuildBase(cfg *config.Config, dir string, progressOutput string) (string, error) {
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
	dockerfileContents, err := generator.GenerateBase()
	if err != nil {
		return "", fmt.Errorf("Failed to generate Dockerfile: %w", err)
	}
	if err := docker.Build(dir, dockerfileContents, imageName, []string{}, false, progressOutput); err != nil {
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
	if err := docker.Build(dir, dockerfileContents, imageName, secrets, noCache, progressOutput); err != nil {
		return fmt.Errorf("Failed to build Docker image for model weights: %w", err)
	}
	return nil
}

func buildRunnerImage(dir, dockerfileContents, dockerignoreContents, imageName string, secrets []string, noCache bool, progressOutput string) error {
	if err := writeDockerignore(dockerignoreContents); err != nil {
		return fmt.Errorf("Failed to write .dockerignore file with weights included: %w", err)
	}
	if err := docker.Build(dir, dockerfileContents, imageName, secrets, noCache, progressOutput); err != nil {
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
	if err := os.Rename(".dockerignore", dockerignoreBackupPath); err != nil {
		return err
	}

	return nil
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

	if err := os.Rename(dockerignoreBackupPath, ".dockerignore"); err != nil {
		return err
	}
	return nil
}
