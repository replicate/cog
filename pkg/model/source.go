package model

import (
	"os"
	"path/filepath"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/util/console"
)

// Source represents a Cog project ready to build.
// It combines the parsed configuration with the project directory location.
type Source struct {
	Config         *config.Config
	ProjectDir     string
	ConfigFilename string // Base filename like "cog.yaml" or "my-config.yaml"
	Warnings       []config.DeprecationWarning
}

// NewSource loads configuration from the given path and returns a Source.
// The configPath can be a filename (e.g., "cog.yaml") which will be searched
// for in the current directory and parent directories.
func NewSource(configPath string) (*Source, error) {
	if configPath == "" {
		configPath = "cog.yaml"
	}

	// Find the root project directory
	rootDir, err := config.GetProjectDir(configPath)
	if err != nil {
		return nil, err
	}

	// Open and read the config file
	fullPath := filepath.Join(rootDir, configPath)
	f, err := os.Open(fullPath)
	if err != nil {
		return nil, &config.ParseError{Filename: configPath, Err: err}
	}
	defer f.Close()

	result, err := config.Load(f, rootDir)
	if err != nil {
		// Add filename context to parse errors if not already present
		if parseErr, ok := err.(*config.ParseError); ok && parseErr.Filename == "" {
			parseErr.Filename = configPath
		}
		return nil, err
	}

	// Display deprecation warnings
	for _, w := range result.Warnings {
		console.Warnf("%s", w.Error())
	}

	return &Source{
		Config:         result.Config,
		ProjectDir:     result.RootDir,
		ConfigFilename: filepath.Base(configPath),
		Warnings:       result.Warnings,
	}, nil
}

// NewSourceFromConfig creates a Source from an existing Config.
// Use this when you already have a parsed config and know the project directory.
func NewSourceFromConfig(cfg *config.Config, projectDir string) *Source {
	return &Source{
		Config:         cfg,
		ProjectDir:     projectDir,
		ConfigFilename: "cog.yaml",
	}
}
