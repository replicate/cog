package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/replicate/cog/pkg/docker"
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

	cmd.AddCommand(newWeightsImportCommand())
	cmd.AddCommand(newWeightsBuildCommand())
	cmd.AddCommand(newWeightsInspectCommand())
	cmd.AddCommand(newWeightsPushCommand())
	cmd.AddCommand(newWeightsStatusCommand())
	return cmd
}

func newWeightsImportCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import [name...]",
		Short: "Build and push weights to a registry",
		Long: `Packages weight sources from cog.yaml into OCI layers and pushes them to a registry
in a single step. Equivalent to running 'cog weights build' followed by 'cog weights push'.

If weight names are provided, only those weights are imported. Otherwise all weights
defined in cog.yaml are imported.

The registry is determined from the image name, which can be:
- Set in cog.yaml as the 'image' field
- Overridden with the --image flag`,
		Args: cobra.ArbitraryArgs,
		RunE: weightsImportCommand,
	}

	addConfigFlag(cmd)
	cmd.Flags().String("image", "", "Registry repository (overrides cog.yaml image field)")
	return cmd
}

func weightsImportCommand(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	src, err := model.NewSource(configFilename)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	cfg := src.Config

	imageName, _ := cmd.Flags().GetString("image")
	if imageName == "" {
		imageName = cfg.Image
	}
	if imageName == "" {
		return fmt.Errorf("To import weights, you must either set the 'image' option in cog.yaml or pass --image. For example, 'cog weights import --image registry.example.com/your-username/model-name'")
	}

	repo, err := parseRepoOnly(imageName)
	if err != nil {
		return err
	}

	weightSpecs, err := collectWeightSpecs(src, args)
	if err != nil {
		return err
	}

	console.Infof("Building %d weight(s)...", len(weightSpecs))

	lockPath := filepath.Join(src.ProjectDir, model.WeightsLockFilename)
	builder := model.NewWeightBuilder(src, lockPath)

	artifacts, err := buildWeightArtifacts(ctx, builder, weightSpecs)
	if err != nil {
		return err
	}

	for _, wa := range artifacts {
		console.Infof("  %s -> %s (%d layer(s), %s)",
			wa.Name(), wa.Target, len(wa.Layers), formatSize(wa.TotalSize()))
	}

	console.Infof("\nPushing %d weight(s) to %s...", len(artifacts), repo)

	return pushWeightArtifacts(ctx, repo, artifacts, "Imported")
}

// collectWeightSpecs extracts WeightSpecs from the source, optionally
// filtered to only the names listed in filterNames. An error is returned
// if no weights match or if a requested name doesn't exist.
func collectWeightSpecs(src *model.Source, filterNames []string) ([]*model.WeightSpec, error) {
	if len(src.Config.Weights) == 0 {
		return nil, fmt.Errorf("no weights defined in %s", configFilename)
	}

	var allSpecs []*model.WeightSpec
	for _, spec := range src.ArtifactSpecs() {
		if ws, ok := spec.(*model.WeightSpec); ok {
			allSpecs = append(allSpecs, ws)
		}
	}

	if len(filterNames) == 0 {
		return allSpecs, nil
	}

	specMap := make(map[string]*model.WeightSpec, len(allSpecs))
	for _, ws := range allSpecs {
		specMap[ws.Name()] = ws
	}

	seen := make(map[string]bool, len(filterNames))
	filtered := make([]*model.WeightSpec, 0, len(filterNames))
	for _, n := range filterNames {
		if seen[n] {
			continue
		}
		seen[n] = true

		ws, ok := specMap[n]
		if !ok {
			return nil, fmt.Errorf("weight %q not found in %s", n, configFilename)
		}
		filtered = append(filtered, ws)
	}
	return filtered, nil
}

// buildWeightArtifacts builds each weight spec into a WeightArtifact via
// the builder, returning the results in the same order as the input specs.
func buildWeightArtifacts(ctx context.Context, builder *model.WeightBuilder, specs []*model.WeightSpec) ([]*model.WeightArtifact, error) {
	artifacts := make([]*model.WeightArtifact, 0, len(specs))
	for _, ws := range specs {
		artifact, err := builder.Build(ctx, ws)
		if err != nil {
			return nil, fmt.Errorf("failed to build weight %q: %w", ws.Name(), err)
		}
		wa, ok := artifact.(*model.WeightArtifact)
		if !ok {
			return nil, fmt.Errorf("unexpected artifact type %T for weight %q", artifact, ws.Name())
		}
		artifacts = append(artifacts, wa)
	}
	return artifacts, nil
}

// parseRepoOnly parses an image string as a bare repository, rejecting
// tags and digests (weight tags are auto-generated).
func parseRepoOnly(imageName string) (string, error) {
	parsedRepo, err := name.NewRepository(imageName, name.Insecure)
	if err != nil {
		if ref, refErr := name.ParseReference(imageName, name.Insecure); refErr == nil {
			return "", fmt.Errorf("image reference %q includes a tag or digest — provide only the repository (e.g., %q)", imageName, ref.Context().Name())
		}
		return "", fmt.Errorf("invalid repository %q: %w", imageName, err)
	}
	return parsedRepo.Name(), nil
}

// pushWeightArtifacts pushes weight artifacts to the registry with
// concurrent layer uploads and progress display. The verb parameter
// controls the summary message (e.g. "Imported" vs "Pushed").
func pushWeightArtifacts(ctx context.Context, repo string, artifacts []*model.WeightArtifact, verb string) error {
	regClient := registry.NewRegistryClient()
	pusher := model.NewWeightPusher(regClient)

	pw := docker.NewProgressWriter()
	defer pw.Close()

	refs := make([]string, len(artifacts))

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(model.GetPushConcurrency())

	for i, wa := range artifacts {
		artName := wa.Name()

		g.Go(func() error {
			result, pushErr := pusher.Push(ctx, repo, wa, model.WeightPushOptions{
				ProgressFn: func(prog model.WeightLayerProgress) {
					row := model.ShortDigest(prog.LayerDigest)
					pw.Write(artName+"/"+row, "Pushing", prog.Complete, prog.Total)
				},
				RetryFn: func(event model.WeightRetryEvent) bool {
					status := fmt.Sprintf("Retrying (%d/%d) in %s",
						event.Attempt, event.MaxAttempts,
						event.NextRetryIn.Round(time.Second))
					pw.WriteStatus(event.Name, status)
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
			refs[i] = result.Ref
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return err
	}

	var totalSize int64
	for i, wa := range artifacts {
		console.Infof("  %s: %s", wa.Name(), refs[i])
		totalSize += wa.TotalSize()
	}

	console.Infof("\n%s %d weight artifact(s) to %s", verb, len(artifacts), repo)
	console.Infof("Total: %s", formatSize(totalSize))

	return nil
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

	weightSpecs, err := collectWeightSpecs(src, nil)
	if err != nil {
		return err
	}

	console.Infof("Processing %d weight source(s)...", len(weightSpecs))

	lockPath := filepath.Join(src.ProjectDir, model.WeightsLockFilename)
	builder := model.NewWeightBuilder(src, lockPath)

	artifacts, err := buildWeightArtifacts(ctx, builder, weightSpecs)
	if err != nil {
		return err
	}

	var totalSize int64
	for _, wa := range artifacts {
		totalSize += wa.TotalSize()
		console.Infof("  %s -> %s (%d layer(s), %s)",
			wa.Name(), wa.Target, len(wa.Layers), formatSize(wa.TotalSize()))
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

	repo, err := parseRepoOnly(imageName)
	if err != nil {
		return err
	}

	lockPath := filepath.Join(src.ProjectDir, model.WeightsLockFilename)
	builder := model.NewWeightBuilder(src, lockPath)

	weightSpecs, err := collectWeightSpecs(src, nil)
	if err != nil {
		return err
	}

	artifacts, err := buildWeightArtifacts(ctx, builder, weightSpecs)
	if err != nil {
		return err
	}

	console.Infof("Pushing %d weight(s) to %s...", len(artifacts), repo)

	return pushWeightArtifacts(ctx, repo, artifacts, "Pushed")
}
