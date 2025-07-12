package commonblocks

import (
	"context"

	"github.com/replicate/cog/pkg/cogpack/plan"
	"github.com/replicate/cog/pkg/cogpack/project"
)

// AptBlock installs system packages
type AptBlock struct{}

func (b *AptBlock) Name() string { return "apt" }

func (b *AptBlock) Detect(ctx context.Context, src *project.SourceInfo) (bool, error) {
	return len(src.Config.Build.SystemPackages) > 0, nil
}

func (b *AptBlock) Dependencies(ctx context.Context, src *project.SourceInfo) ([]plan.Dependency, error) {
	return nil, nil
}

func (b *AptBlock) Plan(ctx context.Context, src *project.SourceInfo, p *plan.Plan) error {
	// stablePackages := slicesext.StableSort(src.Config.Build.SystemPackages)

	// [reference] from buildkit prototype
	// aptCache := llb.AsPersistentCacheDir("apt-cache", llb.CacheMountLocked)
	// pkgList := strings.Join(packages, " ")

	// // 1. apt-get update
	// intermediate := state.Run(
	// 	llb.Shlex("apt-get update -qq"),
	// 	llb.AddMount("/var/cache/apt", llb.Scratch(), aptCache),
	// 	llb.WithCustomName("apt-update"),
	// ).Root()

	// // 2. apt-get install
	// intermediate = intermediate.Run(
	// 	llb.Shlex(fmt.Sprintf("apt-get install -qqy --no-install-recommends %s", pkgList)),
	// 	llb.AddMount("/var/cache/apt", llb.Scratch(), aptCache),
	// 	llb.WithCustomNamef("apt-install %s", pkgList),
	// ).Root()

	// // 3. cleanup
	// intermediate = intermediate.Run(
	// 	llb.Shlex("apt-get clean"),
	// 	llb.WithCustomName("apt-clean"),
	// ).Root()

	// removeDirs := []string{
	// 	"/var/lib/apt/lists/*",
	// 	// docker for mac appears to add /root/.cache/rosetta directory, kill it and the cache directory
	// 	"/root/.cache",
	// 	"/var/log/*",
	// 	"/var/cache/apt/*",
	// 	"/var/lib/apt/lists/*",
	// 	"/var/cache/debconf/*",
	// 	"/usr/share/doc-base/*",
	// 	"/usr/share/common-licenses",
	// }

	// intermediate = intermediate.Run(
	// 	llb.Shlex(fmt.Sprintf("sh -c 'rm -rf %s'", strings.Join(removeDirs, " "))),
	// 	llb.WithCustomName(fmt.Sprintf("remove %s", strings.Join(removeDirs, " "))),
	// ).Root()

	// flattened := state.File(
	// 	llb.Copy(llb.Diff(state, intermediate), "/", "/"),
	// 	llb.WithCustomName("install apt dependencies"),
	// )

	installStage, err := p.AddStage(plan.PhaseSystemDeps, "Install System Packages", "apt-install")
	if err != nil {
		return err
	}

	// operations := []plan.Op{
	// 	plan.Exec{
	// 		Command: "apt-get update",
	// 	},
	// 	plan.Exec{
	// 		// --no-upgrade avoids bloat by not upgrading package installed in lower layers. replace this with dep resolution
	// 		Command: "apt-get install -qqy --no-install-recommends --no-upgrade" + strings.Join(stablePackages, " "),
	// 	},
	// 	plan.Exec{
	// 		Command: "apt-get clean",
	// 	},
	// }

	installStage.Source = p.GetPhaseResult(plan.PhaseBase)
	// installStage.Operations = []plan.Op{

	// TODO: Implement apt package installation
	return nil
}
