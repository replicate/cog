package model

import (
	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/util/console"
)

// Source represents a Cog project ready to build.
// It combines the parsed configuration with the project directory location.
type Source struct {
	Config     *config.Config
	ProjectDir string
	Warnings   []config.DeprecationWarning
}

// NewSource loads configuration from the given path and returns a Source.
// The configPath can be a filename (e.g., "cog.yaml") which will be searched
// for in the current directory and parent directories.
func NewSource(configPath string) (*Source, error) {
	result, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}

	// Display deprecation warnings
	for _, w := range result.Warnings {
		console.Warnf("%s", w.Error())
	}

	return &Source{
		Config:     result.Config,
		ProjectDir: result.RootDir,
		Warnings:   result.Warnings,
	}, nil
}

// NewSourceFromConfig creates a Source from an existing Config.
// Use this when you already have a parsed config and know the project directory.
func NewSourceFromConfig(cfg *config.Config, projectDir string) *Source {
	return &Source{Config: cfg, ProjectDir: projectDir}
}
