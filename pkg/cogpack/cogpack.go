package cogpack

import (
	"context"
	"fmt"

	"github.com/replicate/cog/pkg/cogpack/compat"
	"github.com/replicate/cog/pkg/cogpack/core"
	"github.com/replicate/cog/pkg/cogpack/providers"
)

func GenerateBuildPlan(ctx context.Context, sourceInfo *core.SourceInfo) (*core.Result, error) {
	result := &core.Result{
		Plan: &core.Plan{
			Dependencies: []core.Dependency{},
			BuildSteps:   []core.Stage{},
			ExportSteps:  []core.Stage{},
		},
	}

	for _, provider := range providers.Providers() {
		err := provider.Configure(ctx, sourceInfo)
		if err != nil {
			return nil, err
		}

		detected, err := provider.Detect(ctx, sourceInfo)
		if err != nil {
			return nil, err
		}

		if !detected {
			continue
		}

		result.Providers = append(result.Providers, provider.Name())
		if resolver, ok := provider.(core.DependencyResolver); ok {
			deps, err := resolver.Resolve(ctx, sourceInfo)
			if err != nil {
				return nil, err
			}
			result.Plan.Dependencies = append(result.Plan.Dependencies, deps...)
		}

		resolvedDeps, err := resolveDeps(result.Plan.Dependencies)
		if err != nil {
			return nil, err
		}
		result.Dependencies = resolvedDeps

		if err := provider.Plan(ctx, sourceInfo, result.Plan); err != nil {
			return nil, fmt.Errorf("provider %s: %w", provider.Name(), err)
		}
	}

	return result, nil
}

func resolveDeps(deps []core.Dependency) (map[string]core.Dependency, error) {
	resolved := make(map[string]core.Dependency)

	for _, dep := range deps {
		if dep.Name == "python" {
			pythonDep, err := compat.ResolvePython(dep.RequestedVersion)
			if err != nil {
				return nil, err
			}
			dep.ResolvedVersion = pythonDep.Version.String()
		}
		resolved[dep.Name] = dep
	}

	return resolved, nil
}
