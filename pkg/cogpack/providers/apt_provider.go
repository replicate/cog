package providers

import (
	"context"
	"slices"
	"strings"

	"github.com/replicate/cog/pkg/cogpack/core"
)

// APTProvider installs apt packages listed in Packages.
// This provider is internal and registered via init().
// It appends a single Step to the *BuildSteps* slice.
type APTProvider struct {
	sortedPackages []string
}

func (p *APTProvider) Name() string {
	return "apt"
}

func (p *APTProvider) Configure(ctx context.Context, src *core.SourceInfo) error {
	p.sortedPackages = slices.Sorted(slices.Values(src.Config.Build.SystemPackages))
	p.sortedPackages = slices.Compact(p.sortedPackages)
	return nil
}

func (p *APTProvider) Detect(ctx context.Context, src *core.SourceInfo) (bool, error) {
	hasPackages := len(src.Config.Build.SystemPackages) > 0
	return hasPackages, nil
}

func (p *APTProvider) Plan(ctx context.Context, src *core.SourceInfo, plan *core.Plan) error {
	// Create a deterministic layer id based on package list (order matters)
	layerID := "apt-" + strings.Join(p.sortedPackages, "-")

	// Compose apt-get commands; using `DEBIAN_FRONTEND=noninteractive` to silence prompts.
	cmds := []core.Op{
		core.Exec{Shell: true, Args: []string{"apt-get", "update"}},
		core.Exec{Shell: true, Args: append([]string{"DEBIAN_FRONTEND=noninteractive", "apt-get", "install", "-y"}, p.sortedPackages...)},
		core.Exec{Shell: true, Args: []string{"apt-get", "clean"}},
		core.Exec{Shell: true, Args: []string{"rm", "-rf", "/var/lib/apt/lists/*"}},
	}

	step := core.Stage{
		Name:     "apt-packages",
		LayerID:  layerID,
		Inputs:   []core.Input{{Stage: "base-image"}},
		Commands: cmds,
		Provides: []string{"/var/lib/dpkg/status"}, // rough indicator that packages installed
	}

	plan.BuildSteps = append(plan.BuildSteps, step)
	plan.ExportSteps = append(plan.ExportSteps, step)
	return nil
}
