package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/replicate/go/uuid"

	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/provider"
	"github.com/replicate/cog/pkg/provider/setup"
	"github.com/replicate/cog/pkg/registry"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/pkg/weights"
)

func newPushCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "push [IMAGE]",
		Short: "Build and push model in current directory to a Docker registry",
		Long: `Build a Docker image from cog.yaml and push it to a container registry.

Cog can push to any OCI-compliant registry. When pushing to Replicate's
registry (r8.im), run 'cog login' first to authenticate.`,
		Example: `  # Push to Replicate
  cog push r8.im/your-username/my-model

  # Push to any OCI registry
  cog push registry.example.com/your-username/model-name

  # Push with model weights in a separate layer (Replicate only)
  cog push r8.im/your-username/my-model --separate-weights`,
		RunE: push,
		Args: cobra.MaximumNArgs(1),
	}
	addSecretsFlag(cmd)
	addNoCacheFlag(cmd)
	addSeparateWeightsFlag(cmd)
	addSchemaFlag(cmd)
	addUseCudaBaseImageFlag(cmd)
	addDockerfileFlag(cmd)
	addBuildProgressOutputFlag(cmd)
	addUseCogBaseImageFlag(cmd)
	addStripFlag(cmd)
	addPrecompileFlag(cmd)
	addConfigFlag(cmd)

	return cmd
}

func push(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Initialize the provider registry
	setup.Init()

	dockerClient, err := docker.NewClient(ctx)
	if err != nil {
		return err
	}

	src, err := model.NewSource(configFilename)
	if err != nil {
		return err
	}
	defer src.Close()

	if err := weights.CheckDrift(src.ProjectDir, src.Config.Weights); err != nil {
		return err
	}

	if err := validatePushArgs(src.Config.Model, args); err != nil {
		return err
	}

	imageName := src.Config.Image
	if len(args) > 0 {
		imageName = args[0]
	}

	if imageName == "" {
		return fmt.Errorf("To push images, you must either set the 'image' option in cog.yaml or pass an image name as an argument. For example, 'cog push registry.example.com/your-username/model-name'")
	}

	// Look up the provider for the target registry
	p := provider.DefaultRegistry().ForImage(imageName)
	if p == nil {
		return fmt.Errorf("no provider found for image '%s'", imageName)
	}

	pushOpts := provider.PushOptions{
		Image:      imageName,
		Config:     src.Config,
		ProjectDir: src.ProjectDir,
	}

	// Build the image
	buildID, _ := uuid.NewV7()
	annotations := map[string]string{}
	if buildID.String() != "" {
		annotations["run.cog.push_id"] = buildID.String()
	}

	regClient := registry.NewRegistryClient()
	resolver := model.NewResolver(dockerClient, regClient)

	// Build the model
	console.Infof("Building Docker image from environment in cog.yaml as %s...", console.Bold(imageName))
	console.Info("")
	buildOpts := buildOptionsFromFlags(cmd, imageName, annotations)
	m, err := resolver.Build(ctx, src, buildOpts)
	if err != nil {
		// Call PostPush to handle error logging/analytics
		_ = p.PostPush(ctx, pushOpts, err)
		return err
	}

	// Log weights info
	if len(m.Weights) > 0 {
		console.Infof("\n%d managed weight(s)", len(m.Weights))
	}

	// Prefer the resolved bundle ref; fall back to the image ref for FormatImage.
	pushTarget := m.ImageRef()
	if m.Ref != nil {
		pushTarget = m.Ref.String()
	}
	console.Infof("\nPushing to %s...", console.Bold(pushTarget))

	// Set up progress display using Docker's jsonmessage rendering. This uses the
	// same cursor movement and progress display as `docker push`, which handles
	// terminal resizing correctly (each line is erased and rewritten individually,
	// rather than relying on a bulk cursor-up count that can desync on resize).
	pw := docker.NewProgressWriter()
	defer pw.Close()

	pushed, pushErr := resolver.Push(ctx, m, model.PushOptions{
		ImageProgressFn: func(prog model.PushProgress) {
			if prog.Phase != "" {
				switch prog.Phase {
				case model.PushPhaseExporting:
					console.Infof("Exporting image from Docker daemon...")
				case model.PushPhasePushing:
					console.Infof("Pushing layers...")
				}
				return
			}

			pw.Write(model.ShortDigest(prog.LayerDigest), "Pushing", prog.Complete, prog.Total)
		},
		OnFallback: func() {
			// Close progress writer to finalize OCI progress bars before Docker
			// push starts its own output. Without this, stale OCI progress lines
			// remain on screen above Docker's progress output.
			pw.Close()
		},
	})

	pw.Close()

	// Bypass console.InfoUnformatted: it wraps at terminal width and
	// would hard-break the digest refs we want to be copy-pasteable.
	if pushErr == nil && pushed != nil {
		if tree := formatPushResult(pushed); tree != "" {
			_, _ = fmt.Fprintln(os.Stderr)
			_, _ = fmt.Fprintln(os.Stderr, tree)
		}
	}

	// PostPush: the provider handles formatting errors and any
	// provider-specific success output (e.g. the Replicate model URL).
	if err := p.PostPush(ctx, pushOpts, pushErr); err != nil {
		return err
	}

	// If there was a push error but PostPush didn't return one,
	// return a generic error
	if pushErr != nil {
		return fmt.Errorf("failed to push image: %w", pushErr)
	}

	return nil
}

// validatePushArgs runs the user-input checks `cog push` needs before
// touching Docker: resolve the model ref so a malformed COG_MODEL_TAG
// fails in seconds instead of after a multi-minute build, and reject
// the legacy positional [IMAGE] arg in FormatBundle mode where its
// meaning is ambiguous. The arg is still valid for FormatImage models,
// so the rejection is conditional on a resolvable model ref.
func validatePushArgs(configModel string, args []string) error {
	ref, err := model.ResolveModelRef(configModel)
	if err != nil && !errors.Is(err, model.ErrNoModelRef) {
		return err
	}
	if ref != nil && len(args) > 0 {
		return errors.New(
			"positional image argument not supported with 'model' config\n" +
				"  use COG_MODEL to override the full reference\n" +
				"  use COG_MODEL_TAG to override just the tag",
		)
	}
	return nil
}

// formatPushResult renders a tree of the digest-pinned refs published
// by a successful push. Returns "" for nil models or FormatImage with
// no image artifact.
//
// Output uses space-padded columns (survives copy-paste) and has no
// leading or trailing newlines — callers add separators. Refs are
// assumed digest-pinned per Resolver.Push's post-condition; the tests
// assert this invariant.
func formatPushResult(m *model.Model) string {
	if m == nil {
		return ""
	}

	img := m.GetImageArtifact()

	if m.Format != model.FormatBundle {
		if img == nil || img.Reference == "" {
			return ""
		}
		return fmt.Sprintf("  image  %s", img.Reference)
	}

	// Count siblings so we know which row gets └─ vs ├─.
	hasImage := img != nil && img.Reference != ""
	totalChildren := len(m.Weights)
	if hasImage {
		totalChildren++
	}

	// "weight" is the longest kind label; align weight names so refs
	// line up across rows.
	const labelWidth = len("weight")
	nameWidth := 0
	for _, w := range m.Weights {
		if len(w.Name) > nameWidth {
			nameWidth = len(w.Name)
		}
	}

	var b strings.Builder
	if m.Ref != nil {
		fmt.Fprintf(&b, "  %-*s  %s\n", labelWidth, "model", m.Ref.String())
	}

	i := 0
	branch := func() string {
		i++
		if i == totalChildren {
			return "└─"
		}
		return "├─"
	}

	if hasImage {
		fmt.Fprintf(&b, "  %s %-*s  %s\n", branch(), labelWidth, "image", img.Reference)
	}
	for _, w := range m.Weights {
		fmt.Fprintf(&b, "  %s %-*s  %-*s  %s\n", branch(), labelWidth, "weight", nameWidth, w.Name, w.Reference)
	}
	return strings.TrimRight(b.String(), "\n")
}

