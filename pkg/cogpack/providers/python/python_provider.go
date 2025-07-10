package python

import (
	"context"
	"slices"

	"github.com/replicate/cog/pkg/cogpack/core"
)

type PythonProvider struct{}

func (p *PythonProvider) Name() string {
	return "python"
}

func (p *PythonProvider) Configure(ctx context.Context, src *core.SourceInfo) error {
	return nil
}

func (p *PythonProvider) Detect(ctx context.Context, src *core.SourceInfo) (bool, error) {
	globs := []string{
		"*.py",
	}
	pythonDetected := slices.ContainsFunc(globs, src.FS.GlobExists)
	return pythonDetected, nil
}

func (p *PythonProvider) Resolve(ctx context.Context, src *core.SourceInfo) ([]core.Dependency, error) {
	var deps []core.Dependency

	var pythonVersion, source string

	if src.Config.Build.PythonVersion != "" {
		pythonVersion = src.Config.Build.PythonVersion
		source = "cog.yaml"
	}

	if pythonVersion == "" {
		pythonVersion = "3.13"
		source = "default"
	}

	deps = append(deps, core.Dependency{
		Name:             "python",
		Provider:         "python",
		RequestedVersion: pythonVersion,
		Source:           source,
	})

	return deps, nil
}

func (p *PythonProvider) Plan(ctx context.Context, src *core.SourceInfo, plan *core.Plan) error {
	return nil
}
