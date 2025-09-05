package config

import (
	"fmt"
	"os"
	"path"
	"path/filepath"

	"github.com/replicate/cog/pkg/errors"
	"github.com/replicate/cog/pkg/util/files"
)

const maxSearchDepth = 100

// Returns the project's root directory, or the directory specified by the --project-dir flag
func GetProjectDir(configFilename string) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return findProjectRootDir(cwd, configFilename)
}

// Loads and instantiates a Config object
// customDir can be specified to override the default - current working directory
func GetConfig(configFilename string) (*Config, string, error) {
	config, rootDir, err := GetRawConfig(configFilename)
	if err != nil {
		return nil, "", err
	}
	err = config.ValidateAndComplete(rootDir)
	config.filename = configFilename
	return config, rootDir, err
}

func GetRawConfig(configFilename string) (*Config, string, error) {
	// Find the root project directory
	rootDir, err := GetProjectDir(configFilename)

	if err != nil {
		return nil, "", err
	}
	configPath := path.Join(rootDir, configFilename)

	// Then try to load the config file from there
	config, err := loadConfigFromFile(configPath)
	if err != nil {
		return nil, "", err
	}

	return config, rootDir, err
}

// Given a file path, attempt to load a config from that file
func loadConfigFromFile(file string) (*Config, error) {
	exists, err := files.Exists(file)
	if err != nil {
		return nil, err
	}

	if !exists {
		return nil, fmt.Errorf("%s does not exist in %s. Are you in the right directory?", filepath.Base(file), filepath.Dir(file))
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
func findConfigPathInDirectory(dir string, configFilename string) (configPath string, err error) {
	filePath := path.Join(dir, configFilename)
	exists, err := files.Exists(filePath)
	if err != nil {
		return "", fmt.Errorf("Failed to scan directory %s for %s: %s", dir, filePath, err)
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
