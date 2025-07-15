package python

import (
	"context"

	"github.com/replicate/cog/pkg/cogpack/baseimg"
	"github.com/replicate/cog/pkg/cogpack/plan"
	"github.com/replicate/cog/pkg/cogpack/project"
	"github.com/replicate/cog/pkg/cogpack/stacks/commonblocks"
)

// PythonStack orchestrates builds for Python-based projects
type PythonStack struct{}

// Name returns the human-readable name of this stack
func (s *PythonStack) Name() string {
	return "python"
}

// Detect analyzes the project to determine if this is a Python project
func (s *PythonStack) Detect(ctx context.Context, src *project.SourceInfo) (bool, error) {
	// Check for Python indicators
	pythonIndicators := []string{
		"*.py",
		"pyproject.toml",
		"requirements.txt",
		"setup.py",
	}

	var (
		match bool
		err   error
	)

	match, err = src.FS.Match(pythonIndicators...)
	if err != nil {
		return false, err
	}

	// Also check if Python version is explicitly specified in config
	if !match {
		match = src.Config.Build.PythonVersion != ""
	}

	return match, nil
}

// Plan orchestrates the entire build process for Python projects
func (s *PythonStack) Plan(ctx context.Context, src *project.SourceInfo, composer *plan.Composer) error {
	// Phase 1: Compose blocks based on project analysis
	blocks := plan.DetectBlocks(ctx, src, []plan.Block{
		&PythonBlock{},
		// &commonblocks.CudaBlock{},
		&commonblocks.BaseImageBlock{},
		&UvBlock{},
		&CogWheelBlock{},
		// &commonblocks.AptBlock{},
		// &PipBlock{},
		// &TorchBlock{},
		// &commonblocks.SourceCopyBlock{},
	})

	// Phase 2: Collect dependencies from all active blocks
	var allDeps []*plan.Dependency

	for _, block := range blocks {
		deps, err := block.Dependencies(ctx, src)
		if err != nil {
			return err
		}
		allDeps = append(allDeps, deps...)
	}

	// Phase 3: Resolve dependencies
	resolved, err := plan.ResolveDependencies(ctx, allDeps)
	if err != nil {
		return err
	}
	composer.SetDependencies(resolved)

	mappedResolved := make(map[string]string)
	for _, dep := range resolved {
		mappedResolved[dep.Name] = dep.ResolvedVersion
	}

	// Phase 4: Select base image based on resolved dependencies
	baseImage, err := baseimg.SelectBaseImage(mappedResolved)
	if err != nil {
		return err
	}
	composer.SetBaseImage(baseImage)

	// Phase 5: Let active blocks contribute to the plan
	for _, block := range blocks {
		if err := block.Plan(ctx, src, composer); err != nil {
			return err
		}
	}

	// Phase 6: Set export configuration for runtime image
	composer.SetExportConfig(&plan.ExportConfig{
		Entrypoint:   []string{"python", "-m", "cog.server.http"},
		Cmd:          []string{},
		WorkingDir:   "/src",
		ExposedPorts: map[string]struct{}{"5000/tcp": {}},
		Labels: map[string]string{
			"org.opencontainers.image.title": "Cog Model",
		},
	})

	return nil
}
