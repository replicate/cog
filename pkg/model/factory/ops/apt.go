package ops

import (
	"fmt"
	"strings"

	"github.com/moby/buildkit/client/llb"

	"github.com/replicate/cog/pkg/model/factory/types"
)

// AptInstall installs system packages using apt-get
type AptInstall struct {
	packages []string
}

// NewAptInstall creates a new apt install operation
func NewAptInstall(packages ...string) *AptInstall {
	return &AptInstall{packages: packages}
}

// // NewAptInstallFromConfig creates apt install operation from cog config
// func NewAptInstallFromConfig() *AptInstall {
// 	return &AptInstall{}
// }

func (op *AptInstall) Name() string {
	if len(op.packages) > 0 {
		return fmt.Sprintf("apt-install %s", strings.Join(op.packages, " "))
	}
	return "apt-install-from-config"
}

func (op *AptInstall) ShouldRun(ctx types.Context, state types.State) bool {
	if len(op.packages) > 0 {
		return true
	}
	// Check config for packages
	return len(ctx.Config.Build.SystemPackages) > 0
}

func (op *AptInstall) Apply(ctx types.Context, state llb.State) (llb.State, error) {
	packages := op.packages
	if len(packages) == 0 {
		packages = ctx.Config.Build.SystemPackages
	}

	if len(packages) == 0 {
		return state, nil
	}

	aptCache := llb.AsPersistentCacheDir("apt-cache", llb.CacheMountLocked)
	pkgList := strings.Join(packages, " ")

	// 1. apt-get update
	intermediate := state.Run(
		llb.Shlex("apt-get update -qq"),
		llb.AddMount("/var/cache/apt", llb.Scratch(), aptCache),
		llb.WithCustomName("apt-update"),
	).Root()

	// 2. apt-get install
	intermediate = intermediate.Run(
		llb.Shlex(fmt.Sprintf("apt-get install -qqy --no-install-recommends %s", pkgList)),
		llb.AddMount("/var/cache/apt", llb.Scratch(), aptCache),
		llb.WithCustomNamef("apt-install %s", pkgList),
	).Root()

	// 3. cleanup
	intermediate = intermediate.Run(
		llb.Shlex("apt-get clean"),
		llb.WithCustomName("apt-clean"),
	).Root()

	removeDirs := []string{
		"/var/lib/apt/lists/*",
		// docker for mac appears to add /root/.cache/rosetta directory, kill it and the cache directory
		"/root/.cache",
		"/var/log/*",
		"/var/cache/apt/*",
		"/var/lib/apt/lists/*",
		"/var/cache/debconf/*",
		"/usr/share/doc-base/*",
		"/usr/share/common-licenses",
	}

	intermediate = intermediate.Run(
		llb.Shlex(fmt.Sprintf("sh -c 'rm -rf %s'", strings.Join(removeDirs, " "))),
		llb.WithCustomName(fmt.Sprintf("remove %s", strings.Join(removeDirs, " "))),
	).Root()

	flattened := state.File(
		llb.Copy(llb.Diff(state, intermediate), "/", "/"),
		llb.WithCustomName("install apt dependencies"),
	)

	return flattened, nil
}
