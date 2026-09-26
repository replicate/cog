package cli

import (
	"context"
	"encoding/json"
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
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "push [TARGET]",
		Short: "Build and push model in current directory to a Docker registry",
		Long: `Build from cog.yaml and push to an OCI-compliant registry. Run 'cog login'
first when pushing to Replicate's registry (r8.im).

TARGET overrides the configured destination and all COG_MODEL* environment
variables. It doesn't change the project format: projects configured with
'image' push an image, while projects configured with 'model' push an OCI
bundle. Push targets must use tags, not digests. Untagged image targets use
Docker's default 'latest' tag; untagged bundle targets get a timestamp tag.

With --json, Cog writes one versioned JSON result to stdout after the entire
push succeeds. Progress, warnings, and diagnostics continue on stderr. Every
reference in the result is digest-pinned.`,
		Example: `  # Push an image to Replicate
  cog push r8.im/your-username/my-model

  # Push an image to any OCI registry
  cog push registry.example.com/your-username/model-name

  # Push a bundle project and print its immutable references as JSON
  cog push registry.example.com/your-username/model-name:v1 --json

  # Push with model weights in a separate image layer (Replicate only)
  cog push r8.im/your-username/my-model --separate-weights`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return push(cmd, args, jsonOutput)
		},
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
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output the pushed references as JSON")

	return cmd
}

func push(cmd *cobra.Command, args []string, jsonOutput bool) error {
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

	// Resolve up front so malformed destinations fail before a
	// multi-minute Docker build. The resolved destination is also passed
	// into Build so it can't disagree on a generated timestamp tag.
	destination, err := resolvePushDestination(src.Config.Image, src.Config.Model, args)
	if err != nil {
		return err
	}
	pushTarget := destination.target

	// Look up the provider for the target registry
	p := provider.DefaultRegistry().ForImage(pushTarget)
	if p == nil {
		return fmt.Errorf("no provider found for image '%s'", pushTarget)
	}

	pushOpts := provider.PushOptions{
		Image:      pushTarget,
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
	console.Infof("Building Docker image from environment in cog.yaml as %s...", console.Bold(pushTarget))
	console.Info("")
	buildOpts := buildOptionsFromFlags(cmd, pushTarget, annotations)
	buildOpts.Format = destination.format
	buildOpts.ModelRef = destination.modelRef
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
	announceTarget := m.ImageRef()
	if m.Ref != nil {
		announceTarget = m.Ref.String()
	}
	console.Infof("\nPushing to %s...", console.Bold(announceTarget))

	// Set up progress display using Docker's jsonmessage rendering. This uses the
	// same cursor movement and progress display as `docker push`, which handles
	// terminal resizing correctly (each line is erased and rewritten individually,
	// rather than relying on a bulk cursor-up count that can desync on resize).
	pw := docker.NewProgressWriter()
	defer pw.Close()

	pushed, pushErr := resolver.Push(ctx, m, model.PushOptions{
		RequireDigest: jsonOutput,
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

	return completePush(ctx, p, pushOpts, pushed, pushErr, jsonOutput, console.Output)
}

func completePush(
	ctx context.Context,
	p provider.Provider,
	pushOpts provider.PushOptions,
	pushed *model.Model,
	pushErr error,
	jsonOutput bool,
	output func(string),
) error {
	var jsonResult []byte
	resultErr := pushErr
	if pushErr == nil {
		if jsonOutput {
			jsonResult, resultErr = marshalPushOutput(pushed)
		} else if pushed != nil {
			// Bypass console.InfoUnformatted: it wraps at terminal width and
			// would hard-break the digest refs we want to be copy-pasteable.
			if tree := formatPushResult(pushed); tree != "" {
				_, _ = fmt.Fprintln(os.Stderr)
				_, _ = fmt.Fprintln(os.Stderr, tree)
			}
		}
	}

	// PostPush may add provider-specific diagnostics and success output.
	// JSON stays buffered until it returns successfully.
	if err := p.PostPush(ctx, pushOpts, resultErr); err != nil {
		return err
	}

	if resultErr != nil {
		return fmt.Errorf("failed to push image: %w", resultErr)
	}

	if jsonOutput {
		output(string(jsonResult))
	}
	return nil
}

type pushDestination struct {
	target   string
	format   model.Format
	modelRef *model.ResolvedRef
}

// PushOutput is the versioned machine-readable result of a successful push.
type PushOutput struct {
	Version int                `json:"version"`
	Model   string             `json:"model,omitempty"`
	Image   string             `json:"image"`
	Weights []PushWeightOutput `json:"weights,omitempty"`
}

// PushWeightOutput identifies one managed weight manifest.
type PushWeightOutput struct {
	Name      string `json:"name"`
	Reference string `json:"reference"`
}

// resolvePushDestination resolves and validates the destination before any
// Docker work. A positional target replaces all COG_MODEL* destinations, but
// the configured image/model field still decides the artifact format.
func resolvePushDestination(configImage, configModel string, args []string) (*pushDestination, error) {
	if len(args) > 0 {
		target := args[0]
		if configModel != "" {
			ref, err := resolvedBundleTarget(target)
			if err != nil {
				return nil, err
			}
			return &pushDestination{target: ref.String(), format: model.FormatBundle, modelRef: ref}, nil
		}
		if err := validatePushTarget(target); err != nil {
			return nil, err
		}
		return &pushDestination{target: target, format: model.FormatImage}, nil
	}

	ref, err := model.ResolveModelRef(configImage, configModel)
	if err != nil && !errors.Is(err, model.ErrNoModelRef) {
		return nil, err
	}
	if ref != nil {
		if ref.Digest != "" {
			return nil, fmt.Errorf("cannot push to digest-pinned target %q: use a tag instead", ref.String())
		}
		return &pushDestination{target: ref.String(), format: model.FormatBundle, modelRef: ref}, nil
	}
	if configImage == "" {
		return nil, errors.New("To push images, you must either set the 'image' option in cog.yaml or pass an image name as an argument. For example, 'cog push registry.example.com/your-username/model-name'")
	}
	if err := validatePushTarget(configImage); err != nil {
		return nil, err
	}
	return &pushDestination{target: configImage, format: model.FormatImage}, nil
}

func resolvedBundleTarget(target string) (*model.ResolvedRef, error) {
	parsed, err := model.ParseRef(target, model.Insecure(), model.WithDefaultTag(model.GenerateTimestampTag()))
	if err != nil {
		return nil, err
	}
	if parsed.IsDigest() {
		return nil, fmt.Errorf("cannot push to digest-pinned target %q: use a tag instead", target)
	}
	if err := model.ValidateTag(parsed.Tag()); err != nil {
		return nil, fmt.Errorf("invalid bundle push target %q: tag %q: %w", target, parsed.Tag(), err)
	}
	return &model.ResolvedRef{
		Registry: parsed.Registry(),
		Repo:     parsed.Repository(),
		Tag:      parsed.Tag(),
	}, nil
}

func validatePushTarget(target string) error {
	parsed, err := model.ParseRef(target, model.Insecure())
	if err != nil {
		return err
	}
	if parsed.IsDigest() {
		return fmt.Errorf("cannot push to digest-pinned target %q: use a tag instead", target)
	}
	return nil
}

func marshalPushOutput(m *model.Model) ([]byte, error) {
	out, err := newPushOutput(m)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("serialize push result: %w", err)
	}
	return data, nil
}

func newPushOutput(m *model.Model) (*PushOutput, error) {
	if m == nil {
		return nil, errors.New("push completed without a model result")
	}
	img := m.GetImageArtifact()
	if img == nil {
		return nil, errors.New("push result has no image artifact")
	}
	if err := validateDigestReference("image", img.Reference); err != nil {
		return nil, err
	}

	out := &PushOutput{Version: 1, Image: img.Reference}
	if m.Format == model.FormatBundle {
		if m.Ref == nil {
			return nil, errors.New("bundle push result has no model reference")
		}
		out.Model = m.Ref.String()
		if err := validateDigestReference("model", out.Model); err != nil {
			return nil, err
		}
	}

	if len(m.Weights) > 0 {
		out.Weights = make([]PushWeightOutput, len(m.Weights))
		for i, weight := range m.Weights {
			if err := validateDigestReference(fmt.Sprintf("weight %q", weight.Name), weight.Reference); err != nil {
				return nil, err
			}
			out.Weights[i] = PushWeightOutput{Name: weight.Name, Reference: weight.Reference}
		}
	}
	return out, nil
}

func validateDigestReference(kind, ref string) error {
	if ref == "" {
		return fmt.Errorf("%s reference is empty", kind)
	}
	parsed, err := model.ParseRef(ref, model.Insecure())
	if err != nil {
		return fmt.Errorf("invalid %s reference %q: %w", kind, ref, err)
	}
	if !parsed.IsDigest() {
		return fmt.Errorf("%s reference %q is not digest-pinned", kind, ref)
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
