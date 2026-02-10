package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/registry"
)

// localWeight tracks the local state of a weight from cog.yaml + weights.lock.
type localWeight struct {
	target   string
	source   string
	lockFile *model.WeightFile
}

// WeightsInspectOutput is the structured output for cog weights inspect --json.
type WeightsInspectOutput struct {
	Reference string               `json:"reference"`
	Weights   []WeightInspectEntry `json:"weights"`
}

// WeightInspectEntry represents one weight's comparison between local and remote state.
type WeightInspectEntry struct {
	Name   string             `json:"name"`
	Status string             `json:"status"` // synced, local-only, remote-only, digest-mismatch, missing-lockfile
	Local  *WeightLocalState  `json:"local,omitempty"`
	Remote *WeightRemoteState `json:"remote,omitempty"`
}

// WeightLocalState represents the local state of a weight from cog.yaml + weights.lock.
type WeightLocalState struct {
	Digest     string `json:"digest"`
	Size       int64  `json:"size"`
	Target     string `json:"target"`
	FileExists bool   `json:"fileExists"`
}

// WeightRemoteLayer represents a single layer in a remote weight manifest.
type WeightRemoteLayer struct {
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
	MediaType string `json:"mediaType"`
}

// WeightRemoteState represents the remote state of a weight from the registry.
type WeightRemoteState struct {
	Ref              string              `json:"ref"`
	Tag              string              `json:"tag"`
	Digest           string              `json:"digest"`
	Size             int64               `json:"size"`
	MediaType        string              `json:"mediaType"`
	Layers           []WeightRemoteLayer `json:"layers,omitempty"`
	MatchedByContent bool                `json:"matchedByContent,omitempty"`
}

func newWeightsInspectCommand() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "inspect <ref>",
		Short: "Compare local weights against remote registry state",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return weightsInspectCommand(cmd, args, jsonOutput)
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")
	addConfigFlag(cmd)

	return cmd
}

func weightsInspectCommand(cmd *cobra.Command, args []string, jsonOutput bool) error {
	ctx := cmd.Context()

	// 1. Load local state
	src, err := model.NewSource(configFilename)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	lockPath := filepath.Join(src.ProjectDir, model.WeightsLockFilename)
	lock, lockErr := model.LoadWeightsLock(lockPath)
	// lockErr is OK — lockfile may not exist yet

	// Build local weight map: name -> (lockfile entry, source file path)
	localWeights := make(map[string]*localWeight)
	for _, w := range src.Config.Weights {
		lw := &localWeight{
			target: w.Target,
			source: w.Source,
		}
		localWeights[w.Name] = lw
	}

	// Fill in lockfile data
	if lockErr == nil && lock != nil {
		for i := range lock.Files {
			f := &lock.Files[i]
			if lw, ok := localWeights[f.Name]; ok {
				lw.lockFile = f
			}
		}
	}

	// 2. Resolve remote state — accept repo only (tags are auto-generated for weights).
	parsedRepo, err := name.NewRepository(args[0], name.Insecure)
	if err != nil {
		if ref, refErr := name.ParseReference(args[0], name.Insecure); refErr == nil {
			return fmt.Errorf("image reference %q includes a tag or digest — provide only the repository (e.g., %q)", args[0], ref.Context().Name())
		}
		return fmt.Errorf("invalid repository %q: %w", args[0], err)
	}
	repo := parsedRepo.Name()

	regClient := registry.NewRegistryClient()
	remoteWeights := resolveWeightsByTag(ctx, repo, localWeights, regClient)

	// 3. Build comparison
	out := &WeightsInspectOutput{
		Reference: repo,
	}

	// Track which remote weights we've matched
	matchedRemote := make(map[string]bool)

	// Process local weights
	for _, w := range src.Config.Weights {
		entry := WeightInspectEntry{Name: w.Name}
		lw := localWeights[w.Name]

		if lw.lockFile == nil {
			// No lockfile entry — needs `cog weights build`
			entry.Status = "missing-lockfile"
			entry.Local = &WeightLocalState{
				Target:     lw.target,
				FileExists: fileExists(filepath.Join(src.ProjectDir, lw.source)),
			}
		} else {
			// Check if source file exists on disk
			exists := fileExists(filepath.Join(src.ProjectDir, lw.source))
			entry.Local = &WeightLocalState{
				Digest:     lw.lockFile.Digest,
				Size:       lw.lockFile.Size,
				Target:     lw.lockFile.Dest,
				FileExists: exists,
			}

			if remote, ok := remoteWeights[w.Name]; ok {
				matchedRemote[w.Name] = true
				entry.Remote = remote

				if remote.MatchedByContent || lw.lockFile.Digest == remote.Digest {
					entry.Status = "synced"
				} else {
					entry.Status = "digest-mismatch"
				}
			} else {
				entry.Status = "local-only"
			}
		}

		out.Weights = append(out.Weights, entry)
	}

	// Add remote-only weights
	for name, remote := range remoteWeights {
		if matchedRemote[name] {
			continue
		}
		out.Weights = append(out.Weights, WeightInspectEntry{
			Name:   name,
			Status: "remote-only",
			Remote: remote,
		})
	}

	// 4. Output
	if jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}

	printWeightsInspectText(out)
	return nil
}

// resolveWeightsByTag checks for each local weight's tag in the registry.
// This is the fallback path when no OCI index exists (e.g., after `cog weights push`
// but before `cog push`).
//
// It looks up the combined tag :weights-<name>-<shortdigest> which encodes both
// the weight name and its content digest. A match means the exact content is synced.
func resolveWeightsByTag(ctx context.Context, repo string, localWeights map[string]*localWeight, reg registry.Client) map[string]*WeightRemoteState {
	result := make(map[string]*WeightRemoteState)
	for weightName, lw := range localWeights {
		if lw.lockFile == nil {
			continue
		}

		tag := model.WeightTag(weightName, lw.lockFile.Digest)
		tagRef := repo + ":" + tag

		// Use GetImage to fetch the full manifest (not just HEAD) so we can read layer sizes.
		img, err := reg.GetImage(ctx, tagRef, nil)
		if err != nil {
			continue
		}

		manifest, err := img.Manifest()
		if err != nil {
			continue
		}

		digest, err := img.Digest()
		if err != nil {
			continue
		}

		rawManifest, err := img.RawManifest()
		if err != nil {
			continue
		}

		state := &WeightRemoteState{
			Ref:              tagRef,
			Tag:              tag,
			Digest:           digest.String(),
			Size:             int64(len(rawManifest)),
			MediaType:        string(manifest.MediaType),
			MatchedByContent: true,
		}

		for _, layer := range manifest.Layers {
			state.Layers = append(state.Layers, WeightRemoteLayer{
				Digest:    layer.Digest.String(),
				Size:      layer.Size,
				MediaType: string(layer.MediaType),
			})
		}

		result[weightName] = state
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func printWeightsInspectText(out *WeightsInspectOutput) {
	fmt.Printf("Weights for: %s\n\n", out.Reference)

	for _, w := range out.Weights {
		if w.Remote != nil && w.Remote.Tag != "" {
			fmt.Printf("  %s  :%s\n", w.Name, w.Remote.Tag)
		} else {
			fmt.Printf("  %s\n", w.Name)
		}
		fmt.Printf("    Status:  %s", w.Status)

		switch w.Status {
		case "local-only":
			fmt.Print(" (not pushed)")
		case "remote-only":
			fmt.Print(" (not in cog.yaml)")
		case "missing-lockfile":
			fmt.Print(" (run cog weights build)")
		}
		fmt.Println()

		if w.Local != nil {
			if w.Local.Digest != "" {
				fmt.Printf("    Local:   %s (%s) -> %s\n", w.Local.Digest, formatSize(w.Local.Size), w.Local.Target)
			} else {
				fmt.Printf("    Local:   (no lockfile entry) -> %s\n", w.Local.Target)
			}
		} else {
			fmt.Println("    Local:   -")
		}

		if w.Remote != nil {
			for _, layer := range w.Remote.Layers {
				fmt.Printf("    Layer:   %s (%s)\n", layer.Digest, formatSize(layer.Size))
			}
		} else {
			fmt.Println("    Remote:  -")
		}

		fmt.Println()
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
