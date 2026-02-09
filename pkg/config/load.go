package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/replicate/cog/pkg/errors"
	"github.com/replicate/cog/pkg/util/files"
)

const maxSearchDepth = 100

// LoadResult contains the loaded config and any warnings.
type LoadResult struct {
	Config   *Config
	Warnings []DeprecationWarning
	RootDir  string
}

// Load parses, validates, and completes a config from an io.Reader.
// The projectDir is used for validation (checking that referenced files exist)
// and for completion (resolving CUDA versions, loading requirements files, etc.).
// Always returns warnings if present, even on success.
func Load(r io.Reader, projectDir string) (*LoadResult, error) {
	// Parse
	cfgFile, err := parse(r)
	if err != nil {
		return nil, err
	}

	// Validate
	validationResult := ValidateConfigFile(cfgFile, WithProjectDir(projectDir))

	// Collect warnings
	warnings := validationResult.Warnings

	// Check for errors
	if validationResult.HasErrors() {
		return nil, validationResult.Err()
	}

	// Convert to Config struct
	config, err := configFileToConfig(cfgFile)
	if err != nil {
		return nil, err
	}

	// Complete (resolve CUDA, load requirements, etc.)
	if err := config.Complete(projectDir); err != nil {
		return nil, err
	}

	return &LoadResult{
		Config:   config,
		Warnings: warnings,
		RootDir:  projectDir,
	}, nil
}

// GetProjectDir returns the project's root directory by searching for
// the config file starting from the current working directory.
func GetProjectDir(configFilename string) (string, error) {
	if configFilename == "" {
		configFilename = "cog.yaml"
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return findProjectRootDir(cwd, configFilename)
}

// findConfigPathInDirectory checks if the config file exists in the given directory.
func findConfigPathInDirectory(dir string, configFilename string) (configPath string, err error) {
	filePath := filepath.Join(dir, configFilename)
	exists, err := files.Exists(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to scan directory %s for %s: %w", dir, filePath, err)
	} else if exists {
		return filePath, nil
	}

	return "", errors.ConfigNotFound(fmt.Sprintf("%s not found in %s", configFilename, dir))
}

// findProjectRootDir walks up the directory tree to find the root of the project.
// The project root is defined as the directory housing a `cog.yaml` file.
func findProjectRootDir(startDir string, configFilename string) (string, error) {
	dir := startDir
	for range maxSearchDepth {
		switch _, err := findConfigPathInDirectory(dir, configFilename); {
		case err != nil && !errors.IsConfigNotFound(err):
			return "", err
		case err == nil:
			return dir, nil
		case dir == "." || dir == "/":
			return "", errors.ConfigNotFound(fmt.Sprintf("%s not found in %s (or in any parent directories)", configFilename, startDir))
		}

		dir = filepath.Dir(dir)
	}

	return "", errors.ConfigNotFound("No cog.yaml found in parent directories.")
}
