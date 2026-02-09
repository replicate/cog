package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/registry"
)

// InspectOutput is the structured output for cog inspect --json.
type InspectOutput struct {
	Reference  string           `json:"reference"`
	Type       string           `json:"type"` // "image" or "index"
	CogVersion string           `json:"cogVersion"`
	Index      *InspectIndex    `json:"index,omitempty"`
	Image      *InspectManifest `json:"image,omitempty"`
}

// InspectIndex represents an OCI index in inspect output.
type InspectIndex struct {
	Reference string            `json:"reference"`
	Digest    string            `json:"digest"`
	MediaType string            `json:"mediaType"`
	Manifests []InspectManifest `json:"manifests"`
}

// InspectManifest represents a manifest entry in inspect output.
type InspectManifest struct {
	Type        string            `json:"type"`           // "image" or "weights"
	Name        string            `json:"name,omitempty"` // weight name from AnnotationWeightName
	Digest      string            `json:"digest"`
	MediaType   string            `json:"mediaType"`
	Size        int64             `json:"size"`
	Platform    string            `json:"platform,omitempty"` // "linux/amd64"
	Target      string            `json:"target,omitempty"`   // weight mount path from AnnotationWeightDest
	Annotations map[string]string `json:"annotations,omitempty"`
	Layers      []InspectLayer    `json:"layers"`
}

// InspectLayer represents a layer in inspect output.
type InspectLayer struct {
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
	MediaType string `json:"mediaType"`
}

func newInspectCommand() *cobra.Command {
	var (
		localOnly  bool
		remoteOnly bool
		jsonOutput bool
		rawOutput  bool
	)

	cmd := &cobra.Command{
		Use:    "inspect <ref>",
		Short:  "Inspect a model image or OCI index",
		Args:   cobra.ExactArgs(1),
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return inspectCommand(cmd, args, localOnly, remoteOnly, jsonOutput, rawOutput)
		},
	}

	cmd.Flags().BoolVar(&localOnly, "local", false, "Only inspect local docker daemon")
	cmd.Flags().BoolVar(&remoteOnly, "remote", false, "Only inspect remote registry")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")
	cmd.Flags().BoolVar(&rawOutput, "raw", false, "Output raw JSON fragments (one per line)")

	return cmd
}

func inspectCommand(cmd *cobra.Command, args []string, localOnly, remoteOnly, jsonOutput, rawOutput bool) error {
	ctx := cmd.Context()

	ref, err := model.ParseRef(args[0])
	if err != nil {
		return err
	}

	dockerClient, err := docker.NewClient(ctx)
	if err != nil {
		return err
	}

	regClient := registry.NewRegistryClient()
	resolver := model.NewResolver(dockerClient, regClient)

	// Build resolve options
	var opts []model.Option
	switch {
	case localOnly:
		opts = append(opts, model.LocalOnly())
	case remoteOnly:
		opts = append(opts, model.RemoteOnly())
	}

	m, err := resolver.Inspect(ctx, ref, opts...)
	if err != nil {
		return err
	}

	// Build output
	out, err := buildInspectOutput(ctx, ref.String(), m, regClient)
	if err != nil {
		return err
	}

	switch {
	case rawOutput:
		return streamRaw(ctx, ref.String(), m, regClient)
	case jsonOutput:
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	default:
		printInspectText(out)
		return nil
	}
}

func buildInspectOutput(ctx context.Context, reference string, m *model.Model, reg registry.Client) (*InspectOutput, error) {
	out := &InspectOutput{
		Reference:  reference,
		CogVersion: m.CogVersion,
	}

	if m.Index != nil {
		out.Type = "index"
		idx := &InspectIndex{
			Reference: m.Index.Reference,
			Digest:    m.Index.Digest,
			MediaType: m.Index.MediaType,
		}

		for _, im := range m.Index.Manifests {
			manifest := buildManifestEntry(im)

			// Try to fetch layers from registry
			layers, err := fetchLayers(ctx, reference, im.Digest, reg)
			if err == nil {
				manifest.Layers = layers
			}

			idx.Manifests = append(idx.Manifests, manifest)
		}

		out.Index = idx
	} else {
		out.Type = "image"
		if m.Image != nil {
			manifest := &InspectManifest{
				Type:   "image",
				Digest: m.Image.Digest,
			}
			if m.Image.Platform != nil {
				parts := []string{m.Image.Platform.OS, m.Image.Platform.Architecture}
				if m.Image.Platform.Variant != "" {
					parts = append(parts, m.Image.Platform.Variant)
				}
				manifest.Platform = strings.Join(parts, "/")
			}

			// Try to fetch layers
			if m.Image.Digest != "" {
				layers, err := fetchLayers(ctx, reference, m.Image.Digest, reg)
				if err == nil {
					manifest.Layers = layers
				}
			}

			out.Image = manifest
		}
	}

	return out, nil
}

func buildManifestEntry(im model.IndexManifest) InspectManifest {
	manifest := InspectManifest{
		Digest:      im.Digest,
		MediaType:   im.MediaType,
		Size:        im.Size,
		Annotations: im.Annotations,
	}

	switch im.Type {
	case model.ManifestTypeWeights:
		manifest.Type = "weights"
		manifest.Name = im.Annotations[model.AnnotationWeightName]
		manifest.Target = im.Annotations[model.AnnotationWeightDest]
	default:
		manifest.Type = "image"
		if im.Platform != nil {
			parts := []string{im.Platform.OS, im.Platform.Architecture}
			if im.Platform.Variant != "" {
				parts = append(parts, im.Platform.Variant)
			}
			manifest.Platform = strings.Join(parts, "/")
		}
	}

	return manifest
}

func fetchLayers(ctx context.Context, reference, digest string, reg registry.Client) ([]InspectLayer, error) {
	// Build a digest reference from the repo
	ref, err := model.ParseRef(reference)
	if err != nil {
		return nil, err
	}
	digestRef := ref.Ref.Context().String() + "@" + digest

	img, err := reg.GetImage(ctx, digestRef, nil)
	if err != nil {
		return nil, err
	}

	manifest, err := img.Manifest()
	if err != nil {
		return nil, err
	}

	var layers []InspectLayer
	for _, l := range manifest.Layers {
		layers = append(layers, InspectLayer{
			Digest:    l.Digest.String(),
			Size:      l.Size,
			MediaType: string(l.MediaType),
		})
	}

	return layers, nil
}

type rawStep struct {
	Step     string      `json:"step"`
	Data     interface{} `json:"data,omitempty"`
	Manifest interface{} `json:"manifest,omitempty"`
}

func streamRaw(ctx context.Context, reference string, m *model.Model, reg registry.Client) error {
	enc := json.NewEncoder(os.Stdout)

	// Step 1: resolve
	_ = enc.Encode(rawStep{
		Step: "resolve",
		Data: map[string]interface{}{
			"reference":  reference,
			"cogVersion": m.CogVersion,
			"type": func() string {
				if m.Index != nil {
					return "index"
				}
				return "image"
			}(),
		},
	})

	if m.Index != nil {
		// Step 2: index
		_ = enc.Encode(rawStep{
			Step: "index",
			Data: map[string]interface{}{
				"digest":    m.Index.Digest,
				"mediaType": m.Index.MediaType,
				"count":     len(m.Index.Manifests),
			},
		})

		// Step 3: per-child manifests
		for _, im := range m.Index.Manifests {
			entry := buildManifestEntry(im)

			ref, err := model.ParseRef(reference)
			if err == nil {
				digestRef := ref.Ref.Context().String() + "@" + im.Digest
				img, err := reg.GetImage(ctx, digestRef, nil)
				if err == nil {
					rawManifest, err := img.RawManifest()
					if err == nil {
						var parsed interface{}
						if jsonErr := json.Unmarshal(rawManifest, &parsed); jsonErr == nil {
							_ = enc.Encode(rawStep{
								Step:     "manifest",
								Data:     entry,
								Manifest: parsed,
							})
							continue
						}
					}
				}
			}

			// Fallback: output without raw manifest
			_ = enc.Encode(rawStep{
				Step: "manifest",
				Data: entry,
			})
		}
	}

	// Final step: model summary
	_ = enc.Encode(rawStep{
		Step: "model",
		Data: map[string]interface{}{
			"reference":  reference,
			"cogVersion": m.CogVersion,
		},
	})

	return nil
}

func printInspectText(out *InspectOutput) {
	fmt.Printf("Model: %s\n", out.Reference)
	if out.Type == "index" {
		fmt.Println("Type:  Model Bundle (OCI Index)")
	} else {
		fmt.Println("Type:  Image")
	}
	fmt.Printf("Cog:   %s\n", out.CogVersion)
	fmt.Println()

	if out.Index != nil {
		// Build the digest reference: repo@sha256:...
		digestRef := out.Index.Digest
		if out.Index.Reference != "" && out.Index.Digest != "" {
			// Extract repo from the reference (strip tag/digest)
			repo := out.Index.Reference
			if idx := strings.LastIndex(repo, ":"); idx != -1 {
				// Only strip if it looks like a tag (no @)
				if !strings.Contains(repo[idx:], "@") {
					repo = repo[:idx]
				}
			}
			digestRef = repo + "@" + out.Index.Digest
		}
		fmt.Printf("Index: %s\n", digestRef)
		fmt.Printf("  Tag:       %s\n", out.Reference)
		fmt.Printf("  Digest:    %s\n", out.Index.Digest)
		fmt.Printf("  MediaType: %s\n", out.Index.MediaType)
		fmt.Printf("  Manifests: %d\n", len(out.Index.Manifests))
		fmt.Println()

		for _, m := range out.Index.Manifests {
			printManifestText(m, "  ")
			fmt.Println()
		}
	} else if out.Image != nil {
		printManifestText(*out.Image, "")
	}
}

func printManifestText(m InspectManifest, indent string) {
	if m.Type == "weights" {
		name := m.Name
		if name == "" {
			name = "(unnamed)"
		}
		fmt.Printf("%s[weights] %s\n", indent, name)
	} else {
		platform := m.Platform
		if platform == "" {
			platform = "(unknown)"
		}
		fmt.Printf("%s[image] %s\n", indent, platform)
	}

	fmt.Printf("%s  Digest: %s\n", indent, m.Digest)

	// Show manifest size + total layer size if layers are available
	if len(m.Layers) > 0 {
		var layerTotal int64
		for _, l := range m.Layers {
			layerTotal += l.Size
		}
		fmt.Printf("%s  Size:   %s (Layers: %s)\n", indent, formatSize(m.Size), formatSize(layerTotal))
	} else {
		fmt.Printf("%s  Size:   %s\n", indent, formatSize(m.Size))
	}

	if m.Target != "" {
		fmt.Printf("%s  Target: %s\n", indent, m.Target)
	}

	if m.MediaType != "" {
		fmt.Printf("%s  Type:   %s\n", indent, m.MediaType)
	}

	if len(m.Layers) > 0 {
		fmt.Printf("%s  Layers: %d\n", indent, len(m.Layers))
		for _, l := range m.Layers {
			fmt.Printf("%s    %s  %s  %s\n", indent, l.Digest, formatSize(l.Size), l.MediaType)
		}
	}
}
