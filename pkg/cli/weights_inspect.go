package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/registry"
)

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

// WeightRemoteState represents the remote state of a weight from the registry.
type WeightRemoteState struct {
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
	MediaType string `json:"mediaType"`
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
	type localWeight struct {
		target   string
		source   string
		lockFile *model.WeightFile
	}
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

	// 2. Resolve remote state
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

	// Remote inspect may fail (model not pushed yet) — that's OK
	var remoteWeights map[string]*WeightRemoteState
	m, remoteErr := resolver.Inspect(ctx, ref, model.RemoteOnly())
	if remoteErr == nil && m.Index != nil {
		remoteWeights = make(map[string]*WeightRemoteState)
		for _, im := range m.Index.Manifests {
			if im.Type != model.ManifestTypeWeights {
				continue
			}
			wName := im.Annotations[model.AnnotationWeightName]
			if wName == "" {
				continue
			}
			remoteWeights[wName] = &WeightRemoteState{
				Digest:    im.Digest,
				Size:      im.Size,
				MediaType: im.MediaType,
			}
		}
	}

	// 3. Build comparison
	out := &WeightsInspectOutput{
		Reference: ref.String(),
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

				if lw.lockFile.Digest == remote.Digest {
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

func printWeightsInspectText(out *WeightsInspectOutput) {
	fmt.Printf("Weights for: %s\n\n", out.Reference)

	for _, w := range out.Weights {
		fmt.Printf("  %s\n", w.Name)
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
			fmt.Printf("    Remote:  %s (%s)\n", w.Remote.Digest, formatSize(w.Remote.Size))
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
