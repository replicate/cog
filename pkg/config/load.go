package config

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"

	"github.com/replicate/cog/pkg/errors"
	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/util/files"
)

const maxSearchDepth = 100

// Public ///////////////////////////////////////

// Returns the project's root directory, or the directory specified by the --project-dir flag
func GetProjectDir(projectDirFlag string) (string, error) {
	if projectDirFlag != "" {
		return projectDirFlag, nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return findProjectRootDir(cwd)
}

// Loads and instantiates a Config object
// projectDir can be specified to override the default - current working directory
func GetConfig(projectDir string) (*model.Config, string, error) {
	var rootDir string
	if projectDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, "", err
		}

		// First find the project root directory
		rootDir, err = findProjectRootDir(cwd)
		if err != nil {
			return nil, "", err
		}
	} else {
		rootDir = projectDir
	}
	configPath := path.Join(rootDir, global.ConfigFilename)

	// Then try to load the config file from there
	config, err := loadConfigFromFile(configPath)
	if err != nil {
		return nil, "", err
	}

	// Finally, validate the loaded config
	err = config.ValidateAndCompleteConfig()

	return config, rootDir, err
}

// Private //////////////////////////////////////

// Given a file path, attempt to load a config from that file
func loadConfigFromFile(file string) (*model.Config, error) {
	exists, err := files.Exists(file)
	if err != nil {
		return nil, err
	}

	if !exists {
		return nil, fmt.Errorf("%s does not exist in %s. Are you in the right directory?", global.ConfigFilename, filepath.Dir(file))
	}

	contents, err := ioutil.ReadFile(file)
	if err != nil {
		return nil, err
	}

	config, err := model.ConfigFromYAML(contents)
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
		configPath, err := findConfigPathInDirectory(dir)
		if err != nil && !errors.IsConfigNotFound(err) {
			return "", err
		} else if err == nil {
			return configPath, nil
		} else if dir == "." || dir == "/" {
			return "", errors.ConfigNotFound(fmt.Sprintf("%s not found in %s (or in any parent directories)", global.ConfigFilename, dir))
		}

		dir = filepath.Dir(dir)
	}

	return "", errors.ConfigNotFound("No cog.yaml found in parent directories.")
}
