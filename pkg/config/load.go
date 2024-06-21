package config

import (
	"fmt"
	"os"
	"path"
	"path/filepath"

	"github.com/replicate/cog/pkg/errors"
	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/util/files"
)

const maxSearchDepth = 100

// Returns the project's root directory, or the directory specified by the --project-dir flag
func GetProjectDir(customDir string) (string, error) {
	if customDir != "" {
		return customDir, nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return findProjectRootDir(cwd)
}

// Loads and instantiates a Config object
// customDir can be specified to override the default - current working directory
func GetConfig(customDir string) (*Config, string, error) {
	// Find the root project directory
	rootDir, err := GetProjectDir(customDir)
	if err != nil {
		return nil, "", err
	}
	configPath := path.Join(rootDir, global.ConfigFilename)

	// Then try to load the config file from there
	config, err := loadConfigFromFile(configPath)
	if err != nil {
		return nil, "", err
	}

	err = config.ValidateAndComplete(rootDir)

	return config, rootDir, err
}

// Given a file path, attempt to load a config from that file
func loadConfigFromFile(file string) (*Config, error) {
	exists, err := files.Exists(file)
	if err != nil {
		return nil, err
	}

	if !exists {
		return nil, fmt.Errorf("%s does not exist in %s. Are you in the right directory?", global.ConfigFilename, filepath.Dir(file))
	}

	contents, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}

	config, err := FromYAML(contents)
	if err != nil {
		return nil, err
	}

	return config, nil

}

// Given a directory, find the cog config file in that directory
func findConfigPathInDirectory(dir string) (configPath string, err error) {
	filePath := path.Join(dir, global.ConfigFilename)
	exists, err := files.Exists(filePath)
	if err != nil {
		return "", fmt.Errorf("Failed to scan directory %s for %s: %s", dir, filePath, err)
	} else if exists {
		return filePath, nil
	}

	return "", errors.ConfigNotFound(fmt.Sprintf("%s not found in %s", global.ConfigFilename, dir))
}

// Walk up the directory tree to find the root of the project.
// The project root is defined as the directory housing a `cog.yaml` file.
func findProjectRootDir(startDir string) (string, error) {
	dir := startDir
	for i := 0; i < maxSearchDepth; i++ {
		switch _, err := findConfigPathInDirectory(dir); {
		case err != nil && !errors.IsConfigNotFound(err):
			return "", err
		case err == nil:
			return dir, nil
		case dir == "." || dir == "/":
			return "", errors.ConfigNotFound(fmt.Sprintf("%s not found in %s (or in any parent directories)", global.ConfigFilename, startDir))
		}

		dir = filepath.Dir(dir)
	}

	return "", errors.ConfigNotFound("No cog.yaml found in parent directories.")
}
