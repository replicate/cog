// Cog Config
package config

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"

	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/util/files"
)

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

// Loads the cog config
func GetConfig(projectDir string) (*model.Config, string, error) {
	var rootDir string
	if projectDir != "" {
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

// Walk up the directory tree to find the root of the project.
// The project root is defined as the directory housing a `cog.yaml` file.
func findProjectRootDir(dir string) (string, error) {
	for dir != "." {
		configPath := path.Join(dir, global.ConfigFilename)
		configExists, err := files.Exists(configPath)
		if err != nil {
			return "", err
		}
		if configExists {
			return dir, nil
		}

		dir = filepath.Dir(dir)
	}

	return "", errors.New("No cog.yaml found in parent directories.")
}
