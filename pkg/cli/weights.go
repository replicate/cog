package cli

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/replicate/cog/pkg/docker"
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

	// Set up progress display using Docker's jsonmessage rendering.
	pw := docker.NewProgressWriter()
	defer pw.Close()

	// Push each weight artifact concurrently using errgroup for
	// bounded concurrency and first-error cancellation.
	type pushResult struct {
		ref  string
		size int64
	}

	ordered := make([]pushResult, len(artifacts))

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(model.GetPushConcurrency())

	for i, wa := range artifacts {
		artName := wa.Name()
		artSize := wa.Descriptor().Size

		g.Go(func() error {
			result, pushErr := pusher.Push(ctx, repo, wa, model.WeightPushOptions{
				ProgressFn: func(prog model.PushProgress) {
					pw.Write(artName, "Pushing", prog.Complete, prog.Total)
				},
				RetryFn: func(event model.WeightRetryEvent) bool {
					status := fmt.Sprintf("Retrying (%d/%d) in %s",
						event.Attempt, event.MaxAttempts,
						event.NextRetryIn.Round(time.Second))
					pw.WriteStatus(event.Name, status)
					// In non-TTY mode, also log the error detail since the
					// progress writer output won't be visible.
					if !console.IsTerminal() {
						console.Warnf("  %s: retrying (%d/%d) in %s: %v",
							event.Name, event.Attempt, event.MaxAttempts,
							event.NextRetryIn.Round(time.Second), event.Err)
					}
					return true
				},
			})

			if pushErr != nil {
				pw.WriteStatus(artName, "FAILED")
				return fmt.Errorf("push weight %q: %w", artName, pushErr)
			}

			pw.WriteStatus(artName, "Pushed")
			ordered[i] = pushResult{ref: result.Ref, size: artSize}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		pw.Close()
		return err
	}

	// Close progress display
	pw.Close()

	// Print final summary
	var totalSize int64
	for i, wa := range artifacts {
		console.Infof("  %s: %s", wa.Name(), ordered[i].ref)
		totalSize += ordered[i].size
	}

	console.Infof("\nPushed %d weight artifact(s) to %s", len(artifacts), repo)
	console.Infof("Total: %s", formatSize(totalSize))

	return nil
}
