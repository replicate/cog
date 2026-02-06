package config

import (
	"fmt"
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

// Load finds, parses, validates, and completes a config.
// This is the main entry point for most callers using the new API.
// Always returns warnings if present, even on success.
func Load(configFilename string) (*LoadResult, error) {
	if configFilename == "" {
		configFilename = "cog.yaml"
	}

	// Find the root project directory
	rootDir, err := GetProjectDir(configFilename)
	if err != nil {
		return nil, err
	}

	return loadFromDir(rootDir, configFilename)
}

// loadFromDir loads a config from a specific directory.
func loadFromDir(dir string, configFilename string) (*LoadResult, error) {
	if configFilename == "" {
		configFilename = "cog.yaml"
	}

	configPath := filepath.Join(dir, configFilename)

	// Parse
	cfgFile, err := parse(configPath)
	if err != nil {
		return nil, err
	}

	// Validate
	validationResult := ValidateConfigFile(cfgFile, WithProjectDir(dir))

	// Collect warnings
	warnings := validationResult.Warnings

	// Check for errors
	if validationResult.HasErrors() {
		return nil, validationResult.Err()
	}

	// Convert to Config struct
	config, err := configFileToConfig(cfgFile, configFilename)
	if err != nil {
		return nil, err
	}

	// Complete (resolve CUDA, load requirements, etc.)
	if err := config.Complete(dir); err != nil {
		return nil, err
	}

	return &LoadResult{
		Config:   config,
		Warnings: warnings,
		RootDir:  dir,
	}, nil
}

// Returns the project's root directory, or the directory specified by the --project-dir flag
func GetProjectDir(configFilename string) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return findProjectRootDir(cwd, configFilename)
}

// Given a directory, find the cog config file in that directory
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

// Walk up the directory tree to find the root of the project.
// The project root is defined as the directory housing a `cog.yaml` file.
func findProjectRootDir(startDir string, configFilename string) (string, error) {
	dir := startDir
	for i := 0; i < maxSearchDepth; i++ {
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
