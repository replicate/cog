package plan

import (
	"context"
	"fmt"
)

// Dependency represents a resolved dependency constraint
type Dependency struct {
	Name             string `json:"name"`              // e.g., "python", "torch"
	Provider         string `json:"provider"`          // block that requested it
	RequestedVersion string `json:"requested_version"` // original constraint
	ResolvedVersion  string `json:"resolved_version"`  // final resolved version
	Source           string `json:"source"`            // where it came from
}

// ResolveDependencies handles all dependency logic: deduplication, conflict resolution, and version resolution.
// It takes a slice of dependency requirements and returns a resolved map.
func ResolveDependencies(ctx context.Context, deps []*Dependency) (map[string]*Dependency, error) {
	// Phase 1: Convert slice to map, handling conflicts
	depMap := make(map[string]*Dependency)
	for _, dep := range deps {
		if existing, exists := depMap[dep.Name]; exists {
			// Handle conflict - for now, use simple "last wins" strategy
			// TODO: Implement proper version constraint resolution
			resolved, err := resolveConflict(existing, dep)
			if err != nil {
				return nil, fmt.Errorf("dependency conflict for %s: %w", dep.Name, err)
			}
			depMap[dep.Name] = resolved
		} else {
			depMap[dep.Name] = dep
		}
	}

	// Phase 2: Resolve versions (mutates ResolvedVersion field)
	for name, dep := range depMap {
		resolvedVersion, err := resolveVersion(ctx, dep)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve %s: %w", name, err)
		}
		dep.ResolvedVersion = resolvedVersion
		depMap[name] = dep
	}

	return depMap, nil
}

// resolveConflict handles conflicts between dependency requirements.
// For the initial implementation, we use a simple strategy.
func resolveConflict(existing, incoming *Dependency) (*Dependency, error) {
	// Simple strategy: if versions are the same, merge metadata
	if existing.RequestedVersion == incoming.RequestedVersion {
		// Prefer more specific source information
		if incoming.Source != "" {
			existing.Source = incoming.Source
		}
		if incoming.Provider != "" {
			existing.Provider = incoming.Provider
		}
		return existing, nil
	}

	// For now, just return an error on version conflicts
	// TODO: Implement semantic version constraint resolution
	return nil, fmt.Errorf(
		"version conflict: %s wants %s (from %s) but %s wants %s (from %s)",
		existing.Provider, existing.RequestedVersion, existing.Source,
		incoming.Provider, incoming.RequestedVersion, incoming.Source,
	)
}

// resolveVersion resolves a dependency requirement to a specific version.
// This is where we would integrate with compatibility matrices and version resolution logic.
func resolveVersion(ctx context.Context, dep *Dependency) (string, error) {
	// For the initial implementation, use simple hardcoded resolution
	// TODO: Integrate with pkg/base_images compatibility matrices

	switch dep.Name {
	case "python":
		return resolvePythonVersion(dep.RequestedVersion)
	case "cuda":
		return resolveCudaVersion(dep.RequestedVersion)
	case "torch":
		return resolveTorchVersion(dep.RequestedVersion)
	default:
		// For unknown dependencies, use the requested version as-is
		if dep.RequestedVersion == "" {
			return "", fmt.Errorf("no version specified for dependency %s", dep.Name)
		}
		return dep.RequestedVersion, nil
	}
}

// resolvePythonVersion resolves Python version constraints
func resolvePythonVersion(requested string) (string, error) {
	// Simple hardcoded resolution for now
	switch requested {
	case "", "3", "3.11":
		return "3.11.8", nil
	case "3.12":
		return "3.12.1", nil
	case "3.13":
		return "3.13.4", nil
	default:
		// Try to use the requested version as-is
		return requested, nil
	}
}

// resolveCudaVersion resolves CUDA version constraints
func resolveCudaVersion(requested string) (string, error) {
	switch requested {
	case "", "11", "11.8":
		return "11.8", nil
	case "12", "12.0":
		return "12.0", nil
	default:
		return requested, nil
	}
}

// resolveTorchVersion resolves PyTorch version constraints
func resolveTorchVersion(requested string) (string, error) {
	switch {
	case requested == "" || requested == "latest":
		return "2.1.0", nil
	case requested == "2.0" || requested == ">=2.0.0":
		return "2.1.0", nil
	default:
		return requested, nil
	}
}
