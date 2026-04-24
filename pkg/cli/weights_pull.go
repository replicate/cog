package cli

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/paths"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/pkg/weights"
	"github.com/replicate/cog/pkg/weights/lockfile"
)

func newWeightsPullCommand() *cobra.Command {
	var (
		verbose       bool
		imageOverride string
	)

	cmd := &cobra.Command{
		Use:   "pull [NAME...]",
		Short: "Populate the local weight cache from the registry",
		Long: `Downloads weight files from the registry into the local content-addressed
cache so 'cog predict' and 'cog run' can mount them at runtime.

If weight names are provided, only those weights are pulled. Otherwise all
weights defined in cog.yaml are pulled.

Files already present in the local cache are skipped — re-running pull is
cheap. The local cache defaults to $HOME/.cache/cog/weights; set
COG_CACHE_DIR (or XDG_CACHE_HOME) to move it elsewhere — useful if your
home directory is on a different filesystem than your project.

Use --verbose to show per-layer and per-file progress.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return weightsPullCommand(cmd, args, verbose, imageOverride)
		},
	}

	addConfigFlag(cmd)
	cmd.Flags().StringVar(&imageOverride, "image", "", "Registry repository (overrides cog.yaml image field)")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show per-layer and per-file progress")
	return cmd
}

func weightsPullCommand(cmd *cobra.Command, args []string, verbose bool, imageOverride string) error {
	ctx := cmd.Context()

	src, err := model.NewSource(configFilename)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	if len(src.Config.Weights) == 0 {
		return fmt.Errorf("no weights defined in %s", configFilename)
	}

	mgr, err := newWeightManager(src, imageOverride)
	if err != nil {
		return err
	}

	if verbose {
		// WeightsStoreDir cannot fail here because newWeightManager
		// already resolved it; ignore the error to keep the log block
		// simple.
		storeDir, _ := paths.WeightsStoreDir() //nolint:errcheck // see comment above
		lockPath := filepath.Join(src.ProjectDir, lockfile.WeightsLockFilename)
		console.Infof("Cache:    %s", storeDir)
		console.Infof("Lockfile: %s", lockPath)
		console.Info("")
	}

	results, err := mgr.Pull(ctx, args, pullEventPrinter(verbose))
	printPullSummary(results, verbose)
	return err
}

// pullEventPrinter returns a PullEvent handler that writes progress to
// the console. Verbose mode adds per-layer / per-file detail.
func pullEventPrinter(verbose bool) func(weights.PullEvent) {
	return func(e weights.PullEvent) {
		switch e.Kind {
		case weights.PullEventWeightStart:
			if e.MissingFiles == 0 {
				console.Infof("Pulling %s... cached (%d/%d files)", e.Weight, e.TotalFiles, e.TotalFiles)
				return
			}
			if verbose {
				console.Infof("Pulling %s -> %s", e.Weight, e.Target)
				console.Infof("  manifest: %s", e.ManifestRef)
				console.Infof("  files:    %d missing / %d total", e.MissingFiles, e.TotalFiles)
			} else {
				console.Infof("Pulling %s... (%d file(s))", e.Weight, e.MissingFiles)
			}
		case weights.PullEventLayerStart:
			if !verbose {
				return
			}
			size := "unknown size"
			if e.LayerSize > 0 {
				size = formatSize(e.LayerSize)
			}
			console.Infof("  layer %s (%s)", model.ShortDigest(e.LayerDigest), size)
		case weights.PullEventFileStored:
			if !verbose {
				return
			}
			console.Infof("    %s (%s) %s", e.FilePath, formatSize(e.FileSize), model.ShortDigest(e.FileDigest))
		case weights.PullEventLayerDone:
			// Layer boundary is implicit from the per-file lines.
		case weights.PullEventWeightDone:
			if e.FullyCached {
				return
			}
			console.Infof("Pulling %s... done (%s, %d file(s), %d layer(s))",
				e.Weight, formatSize(e.BytesFetched), e.FilesFetched, e.LayersFetched)
		}
	}
}

func printPullSummary(results []weights.PullResult, verbose bool) {
	if len(results) == 0 {
		return
	}
	var totalBytes int64
	var totalFiles, totalLayers, cachedWeights int
	for _, r := range results {
		if r.FullyCached {
			cachedWeights++
			continue
		}
		totalBytes += r.BytesFetched
		totalFiles += r.FilesFetched
		totalLayers += r.LayersFetched
	}
	if verbose {
		console.Info("")
	}
	if totalFiles == 0 {
		console.Infof("All %d weight(s) already cached.", len(results))
		return
	}
	console.Infof(
		"Pulled %s across %d file(s) / %d layer(s) for %d weight(s); %d already cached.",
		formatSize(totalBytes), totalFiles, totalLayers, len(results)-cachedWeights, cachedWeights,
	)
}
