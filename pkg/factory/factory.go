//go:build ignore

package factory

import (
	"os"
	"strings"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker/command"
)

type Factory struct {
	dockerProvider command.Command
}

func NewFactory(dockerProvider command.Command) *Factory {
	return &Factory{dockerProvider: dockerProvider}
}

type BuildEnv struct {
	ProjectDir  string
	Config      *config.Config
	Secrets     []string
	ProjectRoot *os.Root
}

func NewBuildEnv(projectDir string, config *config.Config, secrets []string) (*BuildEnv, error) {
	fs, err := os.OpenRoot(projectDir)
	if err != nil {
		return nil, err
	}

	env := &BuildEnv{
		ProjectDir:  projectDir,
		Config:      config,
		Secrets:     secrets,
		ProjectRoot: fs,
	}

	return env, nil
}

type BuildStage interface {
	Configure(plan *BuildSpec) error
	DevStageOps() []BuildOp
	RunStageOps() []BuildOp
}

type AptPackages struct {
	Packages   []string
	CacheMount bool
	NoUpgrades bool
}

func (f *Factory) NewAptStage(noCache, noUpgrades bool) *AptPackages {
	return &AptPackages{
		CacheMount: noCache,
		NoUpgrades: noUpgrades,
	}
}

func (a *AptPackages) Configure(spec *BuildSpec) error {
	spec.SystemPackages = stableSlice(a.Packages)

	return nil
}

func (a *AptPackages) apply(stage *stage) {
	runOpts := runOpts{}

	if a.CacheMount {
		runOpts.mounts = append(runOpts.mounts, "--mount=type=cache,target=/var/cache/apt,sharing=locked")
	}

	runOpts.commands = append(runOpts.commands,
		"apt-get update -qq",
		"apt-get install -qqy --no-install-recommends "+a.cleanedPackageNames(),
		"apt-get clean",
	)

	dirsToRemove := stableSlice([]string{
		// docker for mac appears to add /root/.cache/rosetta directory, kill it and the cache directory
		"/root/.cache/rosetta",
		"/usr/share/common-licenses",
		"/usr/share/doc-base/*",
		"/var/cache/apt/*",
		"/var/cache/debconf/*",
		"/var/lib/apt/lists/*",
		"/var/log/*",
	})
	for _, dir := range dirsToRemove {
		runOpts.commands = append(runOpts.commands, "rm -rf "+dir)
	}

	stage.Run(runOpts)
}

func (a *AptPackages) DevOps() []BuildOp {
	if len(a.Packages) == 0 {
		return []BuildOp{}
	}

	return []BuildOp{a.apply}
}

func (a *AptPackages) RunOps() []BuildOp {
	if len(a.Packages) == 0 {
		return []BuildOp{}
	}

	return []BuildOp{a.apply}
}

func (a *AptPackages) cleanedPackageNames() string {
	packages := stableSlice(a.Packages)
	return strings.Join(packages, " ")
}

type installAptPackagesOp struct {
	noCache    bool
	noUpgrades bool
	packages   []string
}

// func (a *installAptPackagesOp) Apply(stage *stage) {
// 	runOpts := runOpts{}

// 	if a.CacheMount {
// 		runOpts.mounts = append(runOpts.mounts, "--mount=type=cache,target=/var/cache/apt,sharing=locked")
// 	}

// 	runOpts.commands = append(runOpts.commands,
// }

// func (a *AptPackages) Generate(stage *stage) {
// 	if len(a.Packages) == 0 {
// 		return
// 	}

// 	runOpts := runOpts{}

// 	if a.CacheMount {
// 		runOpts.mounts = append(runOpts.mounts, "--mount=type=cache,target=/var/cache/apt,sharing=locked")
// 	}

// 	runOpts.commands = append(runOpts.commands,
// 		"apt-get update -qq",
// 		"apt-get install -qqy --no-install-recommends "+a.cleanedPackageNames(),
// 		"apt-get clean",
// 	)

// 	dirsToRemove := stableSlice([]string{
// 		// docker for mac appears to add /root/.cache/rosetta directory, kill it and the cache directory
// 		"/root/.cache/rosetta",
// 		"/usr/share/common-licenses",
// 		"/usr/share/doc-base/*",
// 		"/var/cache/apt/*",
// 		"/var/cache/debconf/*",
// 		"/var/lib/apt/lists/*",
// 		"/var/log/*",
// 	})
// 	for _, dir := range dirsToRemove {
// 		runOpts.commands = append(runOpts.commands, "rm -rf "+dir)
// 	}

// 	stage.Run(runOpts)
// }
