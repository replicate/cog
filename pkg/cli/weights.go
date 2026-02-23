package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/spf13/cobra"
	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"

	"github.com/replicate/cog/pkg/global"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/registry"
	"github.com/replicate/cog/pkg/util/console"
)

func newWeightsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "weights",
		Short:  "Manage model weights",
		Long:   "Commands for managing model weight files.",
		Hidden: true,
	}

	cmd.AddCommand(newWeightsBuildCommand())
	cmd.AddCommand(newWeightsInspectCommand())
	cmd.AddCommand(newWeightsPushCommand())
	return cmd
}

func newWeightsBuildCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "build",
		Short: "Generate weights.lock from weight sources in cog.yaml",
		Long: `Reads the weights section from cog.yaml, processes each weight source,
and generates a weights.lock file containing metadata (digests, sizes) for each file.`,
		Args: cobra.NoArgs,
		RunE: weightsBuildCommand,
	}

	addConfigFlag(cmd)
	return cmd
}

func weightsBuildCommand(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	src, err := model.NewSource(configFilename)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	if len(src.Config.Weights) == 0 {
		return fmt.Errorf("no weights defined in %s", configFilename)
	}

	// Extract weight specs from the source
	var weightSpecs []*model.WeightSpec
	for _, spec := range src.ArtifactSpecs() {
		if ws, ok := spec.(*model.WeightSpec); ok {
			weightSpecs = append(weightSpecs, ws)
		}
	}

	console.Infof("Processing %d weight source(s)...", len(weightSpecs))

	lockPath := filepath.Join(src.ProjectDir, model.WeightsLockFilename)
	builder := model.NewWeightBuilder(src, global.Version, lockPath)

	// Build each weight artifact (hashes file, updates lockfile)
	var totalSize int64
	for _, ws := range weightSpecs {
		artifact, buildErr := builder.Build(ctx, ws)
		if buildErr != nil {
			return fmt.Errorf("failed to build weight %q: %w", ws.Name(), buildErr)
		}

		wa, ok := artifact.(*model.WeightArtifact)
		if !ok {
			return fmt.Errorf("unexpected artifact type %T for weight %q", artifact, ws.Name())
		}
		size := wa.Descriptor().Size
		totalSize += size
		console.Infof("  %s -> %s (%s)", wa.Name(), wa.Target, formatSize(size))
	}

	console.Infof("\nGenerated %s with %d file(s) (%s total)",
		model.WeightsLockFilename, len(weightSpecs), formatSize(totalSize))

	return nil
}

func formatSize(bytes int64) string {
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
	)

	switch {
	case bytes >= gb:
		return fmt.Sprintf("%.1fGB", float64(bytes)/float64(gb))
	case bytes >= mb:
		return fmt.Sprintf("%.1fMB", float64(bytes)/float64(mb))
	case bytes >= kb:
		return fmt.Sprintf("%.1fKB", float64(bytes)/float64(kb))
	default:
		return fmt.Sprintf("%dB", bytes)
	}
}

func newWeightsPushCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "push [IMAGE]",
		Short: "Push weights to a registry",
		Long: `Reads weights.lock and pushes weight files as an OCI artifact to a registry.

The registry is determined from the image name, which can be:
- Specified as an argument: cog weights push registry.example.com/user/model
- Set in cog.yaml as the 'image' field`,
		Args: cobra.MaximumNArgs(1),
		RunE: weightsPushCommand,
	}

	addConfigFlag(cmd)
	return cmd
}

func weightsPushCommand(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	src, err := model.NewSource(configFilename)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	cfg := src.Config

	// Determine image name
	imageName := cfg.Image
	if len(args) > 0 {
		imageName = args[0]
	}
	if imageName == "" {
		return fmt.Errorf("To push weights, you must either set the 'image' option in cog.yaml or pass an image name as an argument. For example, 'cog weights push registry.example.com/your-username/model-name'")
	}

	// Parse as repository only — reject tags/digests since weight tags are auto-generated.
	parsedRepo, err := name.NewRepository(imageName, name.Insecure)
	if err != nil {
		// NewRepository fails for inputs with :tag or @digest — check if it's a valid ref
		if ref, refErr := name.ParseReference(imageName, name.Insecure); refErr == nil {
			return fmt.Errorf("image reference %q includes a tag or digest — provide only the repository (e.g., %q)", imageName, ref.Context().Name())
		}
		return fmt.Errorf("invalid repository %q: %w", imageName, err)
	}
	repo := parsedRepo.Name()

	if len(cfg.Weights) == 0 {
		return fmt.Errorf("no weights defined in %s", configFilename)
	}

	// Build weight artifacts (reads lockfile as cache, hashes files)
	lockPath := filepath.Join(src.ProjectDir, model.WeightsLockFilename)
	builder := model.NewWeightBuilder(src, global.Version, lockPath)

	var artifacts []*model.WeightArtifact
	for _, spec := range src.ArtifactSpecs() {
		ws, ok := spec.(*model.WeightSpec)
		if !ok {
			continue
		}
		artifact, buildErr := builder.Build(ctx, ws)
		if buildErr != nil {
			return fmt.Errorf("failed to build weight %q: %w", ws.Name(), buildErr)
		}
		wa, ok := artifact.(*model.WeightArtifact)
		if !ok {
			return fmt.Errorf("unexpected artifact type %T for weight %q", artifact, ws.Name())
		}
		artifacts = append(artifacts, wa)
	}

	if len(artifacts) == 0 {
		return fmt.Errorf("no weight artifacts to push")
	}

	console.Infof("Pushing %d weight file(s) to %s...", len(artifacts), repo)

	regClient := registry.NewRegistryClient()
	pusher := model.NewWeightPusher(regClient)

	// Set up mpb progress container (writes to stderr, auto-detects TTY)
	p := mpb.New(mpb.WithOutput(os.Stderr), mpb.WithWidth(80))

	// Track per-artifact retry status for dynamic decorator display
	var retryStatus sync.Map // name -> string

	// Create a bar for each artifact
	type barEntry struct {
		bar  *mpb.Bar
		name string
		size int64
	}
	bars := make([]barEntry, len(artifacts))
	for i, wa := range artifacts {
		artName := wa.Name()
		displayName := artName
		if len(displayName) > 30 {
			displayName = "..." + displayName[len(displayName)-27:]
		}
		total := wa.Descriptor().Size
		if total <= 0 {
			total = 1 // avoid zero-total bars; will be updated by SetTotal
		}

		// Capture artName for the closure
		retryStatus.Store(artName, "")
		statusFn := func(s decor.Statistics) string {
			if v, ok := retryStatus.Load(artName); ok {
				if msg, _ := v.(string); msg != "" {
					return msg
				}
			}
			return ""
		}

		bar := p.AddBar(total,
			mpb.PrependDecorators(
				decor.Name(fmt.Sprintf("  %-30s", displayName), decor.WC{C: decor.DindentRight}),
			),
			mpb.AppendDecorators(
				decor.OnAbort(
					decor.OnComplete(
						decor.CountersKibiByte("% .1f / % .1f", decor.WCSyncWidth),
						"done",
					),
					"FAILED",
				),
				decor.OnAbort(
					decor.OnComplete(
						decor.Percentage(decor.WC{W: 6}),
						"",
					),
					"",
				),
				// Dynamic retry status decorator
				decor.Any(statusFn, decor.WC{W: 1}),
			),
			mpb.BarFillerOnComplete(""),
		)
		bars[i] = barEntry{bar: bar, name: artName, size: wa.Descriptor().Size}
	}

	// Push each weight artifact concurrently
	type pushResult struct {
		name string
		ref  string
		size int64
		err  error
	}

	sem := make(chan struct{}, model.GetPushConcurrency())
	results := make(chan pushResult, len(artifacts))
	var wg sync.WaitGroup

	for i, wa := range artifacts {
		wg.Add(1)
		go func(wa *model.WeightArtifact, entry barEntry) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			result, pushErr := pusher.Push(ctx, repo, wa, model.WeightPushOptions{
				ProgressFn: func(prog model.PushProgress) {
					retryStatus.Store(entry.name, "")
					if prog.Total > 0 {
						entry.bar.SetTotal(prog.Total, false)
					}
					entry.bar.SetCurrent(prog.Complete)
				},
				RetryFn: func(event model.WeightRetryEvent) bool {
					msg := fmt.Sprintf("  retry %d/%d in %s",
						event.Attempt, event.MaxAttempts,
						event.NextRetryIn.Round(time.Second))
					retryStatus.Store(event.Name, msg)
					entry.bar.SetCurrent(0)
					console.Warnf("  %s: retrying (%d/%d) in %s: %v",
						event.Name, event.Attempt, event.MaxAttempts,
						event.NextRetryIn.Round(time.Second), event.Err)
					return true
				},
			})

			if pushErr != nil {
				entry.bar.Abort(false) // keep the bar visible, show FAILED
				results <- pushResult{name: entry.name, err: pushErr}
			} else {
				entry.bar.SetTotal(entry.bar.Current(), true) // mark complete
				results <- pushResult{name: entry.name, ref: result.Ref, size: entry.size}
			}
		}(wa, bars[i])
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	var totalSize int64
	var errorCount int
	refs := make(map[string]string) // name -> ref
	for r := range results {
		if r.err != nil {
			errorCount++
		} else {
			totalSize += r.size
			refs[r.name] = r.ref
		}
	}

	// Wait for mpb to finish rendering
	p.Wait()

	// Print final summary
	for _, entry := range bars {
		if ref, ok := refs[entry.name]; ok {
			console.Infof("  %s: %s", entry.name, ref)
		}
	}

	if errorCount > 0 {
		return fmt.Errorf("failed to push %d/%d weight files", errorCount, len(artifacts))
	}

	console.Infof("\nPushed %d weight artifact(s) to %s", len(artifacts), repo)
	console.Infof("Total: %s", formatSize(totalSize))

	return nil
}
