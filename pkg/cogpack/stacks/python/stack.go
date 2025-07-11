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

	for _, pattern := range pythonIndicators {
		if src.FS.GlobExists(pattern) {
			return true, nil
		}
	}

	// Also check if Python version is explicitly specified in config
	if src.Config.Build.PythonVersion != "" {
		return true, nil
	}

	return false, nil
}

// Plan orchestrates the entire build process for Python projects
func (s *PythonStack) Plan(ctx context.Context, src *project.SourceInfo, p *plan.Plan) error {
	// Phase 1: Compose blocks based on project analysis
	allBlocks := s.composeBlocks(ctx, src)

	// Phase 2: Collect dependencies from all active blocks
	var allDeps []plan.Dependency
	var activeBlocks []plan.Block

	for _, block := range allBlocks {
		if active, err := block.Detect(ctx, src); err != nil {
			return err
		} else if active {
			activeBlocks = append(activeBlocks, block)

			deps, err := block.Dependencies(ctx, src)
			if err != nil {
				return err
			}
			allDeps = append(allDeps, deps...)
		}
	}

	// Phase 3: Resolve dependencies
	resolved, err := plan.ResolveDependencies(ctx, allDeps)
	if err != nil {
		return err
	}
	p.Dependencies = resolved

	mappedResolved := make(map[string]string)
	for _, dep := range resolved {
		mappedResolved[dep.Name] = dep.ResolvedVersion
	}

	// Phase 4: Select base image based on resolved dependencies
	baseImage, err := baseimg.SelectBaseImage(mappedResolved)
	if err != nil {
		return err
	}
	p.BaseImage = baseImage

	// Phase 5: Let active blocks contribute to the plan
	for _, block := range activeBlocks {
		if err := block.Plan(ctx, src, p); err != nil {
			return err
		}
	}

	// Phase 6: Set export configuration for runtime image
	p.Export = &plan.ExportConfig{
		Entrypoint:   []string{"python", "-m", "cog.server.http"},
		Cmd:          []string{},
		WorkingDir:   "/src",
		ExposedPorts: map[string]struct{}{"5000/tcp": {}},
		Labels: map[string]string{
			"org.opencontainers.image.title": "Cog Model",
		},
	}

	return nil
}

// composeBlocks determines which blocks to use based on project characteristics
func (s *PythonStack) composeBlocks(ctx context.Context, src *project.SourceInfo) (blockList []plan.Block) {
	// Always include base blocks
	blockList = append(blockList, &PythonBlock{})
	blockList = append(blockList, &commonblocks.BaseImageBlock{})

	// System packages if specified
	if len(src.Config.Build.SystemPackages) > 0 {
		blockList = append(blockList, &commonblocks.AptBlock{})
	}

	// Python dependency management - pick one based on project structure
	if src.FS.GlobExists("pyproject.toml") {
		blockList = append(blockList, &UvBlock{})
	} else if src.FS.GlobExists("requirements.txt") {
		blockList = append(blockList, &PipBlock{})
	}

	// ML frameworks - these self-detect if needed
	blockList = append(blockList, &TorchBlock{})
	blockList = append(blockList, &commonblocks.CudaBlock{})

	// Always copy source code to runtime image
	blockList = append(blockList, &commonblocks.SourceCopyBlock{})

	return blockList
}
