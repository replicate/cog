package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/registry"
)

// localWeight tracks the local state of a weight from cog.yaml + weights.lock.
type localWeight struct {
	target   string
	source   string
	lockFile *model.WeightLockEntry
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

// WeightInspectLayer is a single layer descriptor, shared between local
// and remote state. Annotations are not surfaced — they'd just noise up
// the output.
type WeightInspectLayer struct {
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
	MediaType string `json:"mediaType"`
}

// WeightLocalState describes the local lockfile view of a weight.
type WeightLocalState struct {
	// Digest is the weight's assembled manifest digest (spec §3.6).
	Digest string `json:"digest"`
	// Target is the container mount path for the weight.
	Target string `json:"target"`
	// SourceExists reports whether the source directory is still on disk.
	SourceExists bool                 `json:"sourceExists"`
	Layers       []WeightInspectLayer `json:"layers,omitempty"`
}

// WeightRemoteState represents the remote state of a weight from the registry.
type WeightRemoteState struct {
	Ref              string               `json:"ref"`
	Tag              string               `json:"tag"`
	Digest           string               `json:"digest"`
	Size             int64                `json:"size"`
	MediaType        string               `json:"mediaType"`
	Layers           []WeightInspectLayer `json:"layers,omitempty"`
	MatchedByContent bool                 `json:"matchedByContent,omitempty"`
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

	// Build local weight map: name -> (lockfile entry, source directory path)
	localWeights := make(map[string]*localWeight)
	for _, w := range src.Config.Weights {
		localWeights[w.Name] = &localWeight{
			target: w.Target,
			source: w.SourceURI(),
		}
	}

	// Fill in lockfile data
	if lockErr == nil && lock != nil {
		for i := range lock.Weights {
			entry := &lock.Weights[i]
			if lw, ok := localWeights[entry.Name]; ok {
				lw.lockFile = entry
			}
		}
	}

	// 2. Resolve remote state — accept repo only (tags are auto-generated for weights).
	repo, err := parseRepoOnly(args[0])
	if err != nil {
		return err
	}

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
		sourceExists := dirExists(filepath.Join(src.ProjectDir, lw.source))

		if lw.lockFile == nil {
			// No lockfile entry — needs `cog weights build`
			entry.Status = "missing-lockfile"
			entry.Local = &WeightLocalState{
				Target:       lw.target,
				SourceExists: sourceExists,
			}
		} else {
			entry.Local = &WeightLocalState{
				Digest:       lw.lockFile.Digest,
				Target:       lw.lockFile.Target,
				SourceExists: sourceExists,
				Layers:       lockLayersForInspect(lw.lockFile.Layers),
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
// This is the fallback path when no OCI index exists (e.g., after
// `cog weights push` but before `cog push`). The tag encodes the weight
// name plus the short prefix of its manifest digest; a hit means the exact
// content is synced.
//
// Each weight is an independent registry round-trip, so we fan them out
// concurrently. Errors are silently ignored per-weight (same as before).
func resolveWeightsByTag(ctx context.Context, repo string, localWeights map[string]*localWeight, reg registry.Client) map[string]*WeightRemoteState {
	type lookup struct {
		name  string
		state *WeightRemoteState
	}
	results := make(chan lookup, len(localWeights))

	var wg sync.WaitGroup
	sem := make(chan struct{}, model.GetPushConcurrency())
	for weightName, lw := range localWeights {
		if lw.lockFile == nil {
			continue
		}
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			results <- lookup{name: weightName, state: fetchRemoteWeight(ctx, reg, repo, weightName, lw.lockFile.Digest)}
		})
	}
	wg.Wait()
	close(results)

	resolved := make(map[string]*WeightRemoteState)
	for r := range results {
		if r.state != nil {
			resolved[r.name] = r.state
		}
	}
	if len(resolved) == 0 {
		return nil
	}
	return resolved
}

// fetchRemoteWeight returns the remote state for a single weight, or nil if
// the tag isn't present (or any other fetch error — the caller treats
// missing-in-registry as "not synced", not as a hard failure).
func fetchRemoteWeight(ctx context.Context, reg registry.Client, repo, weightName, manifestDigest string) *WeightRemoteState {
	tag := model.WeightTag(weightName, manifestDigest)
	tagRef := repo + ":" + tag

	img, err := reg.GetImage(ctx, tagRef, nil)
	if err != nil {
		return nil
	}
	manifest, err := img.Manifest()
	if err != nil {
		return nil
	}
	digest, err := img.Digest()
	if err != nil {
		return nil
	}
	rawManifest, err := img.RawManifest()
	if err != nil {
		return nil
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
		state.Layers = append(state.Layers, WeightInspectLayer{
			Digest:    layer.Digest.String(),
			Size:      layer.Size,
			MediaType: string(layer.MediaType),
		})
	}
	return state
}

func lockLayersForInspect(in []model.WeightLockLayer) []WeightInspectLayer {
	if len(in) == 0 {
		return nil
	}
	out := make([]WeightInspectLayer, len(in))
	for i, l := range in {
		out[i] = WeightInspectLayer{
			Digest:    l.Digest,
			Size:      l.Size,
			MediaType: l.MediaType,
		}
	}
	return out
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
				fmt.Printf("    Local:   %s -> %s\n", w.Local.Digest, w.Local.Target)
				for _, layer := range w.Local.Layers {
					fmt.Printf("    Layer:   %s (%s)\n", layer.Digest, formatSize(layer.Size))
				}
			} else {
				fmt.Printf("    Local:   (no lockfile entry) -> %s\n", w.Local.Target)
			}
		} else {
			fmt.Println("    Local:   -")
		}

		if w.Remote != nil {
			for _, layer := range w.Remote.Layers {
				fmt.Printf("    Remote:  %s (%s)\n", layer.Digest, formatSize(layer.Size))
			}
		} else {
			fmt.Println("    Remote:  -")
		}

		fmt.Println()
	}
}

// dirExists is true when path is a readable directory on disk. Everything
// else (missing, stat error, regular file) is false.
func dirExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}
