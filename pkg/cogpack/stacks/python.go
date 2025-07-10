package stacks

import (
	"context"

	"github.com/replicate/cog/pkg/cogpack"
	"github.com/replicate/cog/pkg/cogpack/blocks"
	"github.com/replicate/cog/pkg/cogpack/core"
)

// PythonStack orchestrates builds for Python-based projects
type PythonStack struct{}

// Name returns the human-readable name of this stack
func (s *PythonStack) Name() string {
	return "python"
}

// Detect analyzes the project to determine if this is a Python project
func (s *PythonStack) Detect(ctx context.Context, src *core.SourceInfo) (bool, error) {
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
func (s *PythonStack) Plan(ctx context.Context, src *core.SourceInfo, plan *cogpack.Plan) error {
	// Phase 1: Compose blocks based on project analysis
	blocks := s.composeBlocks(ctx, src)

	// Phase 2: Collect dependencies from all active blocks
	var allDeps []cogpack.Dependency
	var activeBlocks []cogpack.Block

	for _, block := range blocks {
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
	resolved, err := cogpack.ResolveDependencies(ctx, allDeps)
	if err != nil {
		return err
	}
	plan.Dependencies = resolved

	// Phase 4: Select base image based on resolved dependencies
	baseImage, err := cogpack.SelectBaseImage(resolved)
	if err != nil {
		return err
	}
	plan.BaseImage = baseImage

	// Phase 5: Let active blocks contribute to the plan
	for _, block := range activeBlocks {
		if err := block.Plan(ctx, src, plan); err != nil {
			return err
		}
	}

	return nil
}

// composeBlocks determines which blocks to use based on project characteristics
func (s *PythonStack) composeBlocks(ctx context.Context, src *core.SourceInfo) []cogpack.Block {
	var blockList []cogpack.Block

	// Always include base blocks
	blockList = append(blockList, &blocks.PythonVersionBlock{})
	blockList = append(blockList, &blocks.BaseImageBlock{})

	// System packages if specified
	if len(src.Config.Build.SystemPackages) > 0 {
		blockList = append(blockList, &blocks.AptBlock{})
	}

	// Python dependency management - pick one based on project structure
	if src.FS.GlobExists("pyproject.toml") {
		blockList = append(blockList, &blocks.UvBlock{})
	} else if src.FS.GlobExists("requirements.txt") {
		blockList = append(blockList, &blocks.PipBlock{})
	}

	// ML frameworks - these self-detect if needed
	blockList = append(blockList, &blocks.TorchBlock{})
	blockList = append(blockList, &blocks.CudaBlock{})

	return blockList
}
