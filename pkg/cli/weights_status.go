package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/registry"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/pkg/weights/lockfile"
)

// The root command has SilenceErrors: true, so Cobra exits 1 without
// printing the error message.
var errWeightsNotReady = errors.New("not all weights are ready")

// WeightsStatusOutput is the top-level structured output for cog weights status --json.
type WeightsStatusOutput struct {
	Weights []WeightStatusEntry `json:"weights"`
}

// WeightStatusEntry is one weight's JSON representation.
type WeightStatusEntry struct {
	Name           string              `json:"name"`
	Target         string              `json:"target"`
	Status         string              `json:"status"`
	Size           int64               `json:"size,omitempty"`
	SizeCompressed int64               `json:"sizeCompressed,omitempty"`
	LayerCount     int                 `json:"layerCount,omitempty"`
	FileCount      int                 `json:"fileCount,omitempty"`
	Digest         string              `json:"digest,omitempty"`
	Source         *WeightStatusSource `json:"source,omitempty"`
	Layers         []LayerStatusEntry  `json:"layers,omitempty"`
}

// WeightStatusSource records the source metadata from the lockfile entry.
type WeightStatusSource struct {
	URI         string `json:"uri,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

// LayerStatusEntry is one layer's status in the output.
type LayerStatusEntry struct {
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
	Status string `json:"status"`
}

func newWeightsStatusCommand() *cobra.Command {
	var (
		jsonOutput bool
		verbose    bool
	)

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the status of configured weights",
		Long: `Shows each declared weight's state across config (cog.yaml), lockfile
(weights.lock), and registry.

Status values:
  ready       - config + lockfile match, all layers in registry
  incomplete  - config + lockfile match, some layers missing from registry
  stale       - lockfile exists but config has drifted
  pending     - declared in config, not yet built
  orphaned    - in lockfile but removed from config

Every non-ready status is resolved by running 'cog weights import'.

Use --verbose to show per-layer status for each weight.

Exit code is 0 when all weights are ready, 1 otherwise.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return weightsStatusCommand(cmd, jsonOutput, verbose)
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show per-layer status")
	addConfigFlag(cmd)

	return cmd
}

func weightsStatusCommand(cmd *cobra.Command, jsonOutput, verbose bool) error {
	ctx := cmd.Context()

	src, err := model.NewSource(configFilename)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	// Load lockfile — missing is fine (weights may not be built yet), but
	// a present-but-corrupt file gets a warning so it doesn't fail silently.
	lockPath := filepath.Join(src.ProjectDir, lockfile.WeightsLockFilename)
	lock, lockErr := lockfile.LoadWeightsLock(lockPath)
	if lockErr != nil && !errors.Is(lockErr, os.ErrNotExist) {
		console.Warnf("Failed to load %s: %s", lockfile.WeightsLockFilename, lockErr)
	}

	// Resolve registry repo — required for status checks.
	if src.Config.Image == "" {
		return fmt.Errorf("no 'image' configured in %s — cannot check registry state", configFilename)
	}
	repo, err := parseRepoOnly(src.Config.Image)
	if err != nil {
		return fmt.Errorf("invalid image %q: %w", src.Config.Image, err)
	}

	reg := registry.NewRegistryClient()

	ws, err := model.ComputeWeightsStatus(ctx, src.Config, lock, repo, reg)
	if err != nil {
		return fmt.Errorf("computing weight status: %w", err)
	}

	// Format output.
	entries := statusResultsToEntries(ws.Results())
	out := &WeightsStatusOutput{Weights: entries}

	if jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}

	printWeightsStatusText(out, verbose)

	if ws.HasProblems() {
		return errWeightsNotReady
	}

	return nil
}

// statusResultsToEntries converts model results to CLI output entries.
func statusResultsToEntries(results []model.WeightStatusResult) []WeightStatusEntry {
	entries := make([]WeightStatusEntry, len(results))
	for i, r := range results {
		entries[i] = WeightStatusEntry{
			Name:   r.Name,
			Target: r.Target,
			Status: r.Status,
		}
		if r.LockEntry != nil {
			le := r.LockEntry
			entries[i].Size = le.Size
			entries[i].SizeCompressed = le.SizeCompressed
			entries[i].LayerCount = len(le.Layers)
			entries[i].FileCount = len(le.Files)
			entries[i].Digest = le.Digest
			entries[i].Source = lockSourceToStatus(le.Source)
		}
		for _, l := range r.Layers {
			entries[i].Layers = append(entries[i].Layers, LayerStatusEntry{
				Digest: l.Digest,
				Size:   l.Size,
				Status: l.Status,
			})
		}
	}
	return entries
}

func lockSourceToStatus(s lockfile.WeightLockSource) *WeightStatusSource {
	fp := string(s.Fingerprint)
	if s.URI == "" && fp == "" {
		return nil
	}
	return &WeightStatusSource{
		URI:         s.URI,
		Fingerprint: fp,
	}
}

func printWeightsStatusText(out *WeightsStatusOutput, verbose bool) {
	if len(out.Weights) == 0 {
		fmt.Println("No weights configured.")
		return
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NAME\tTARGET\tSTATUS\tSIZE\tLAYERS\tDIGEST")

	for _, e := range out.Weights {
		size := "-"
		if e.Size > 0 {
			size = formatSize(e.Size)
		}

		layers := "-"
		if e.LayerCount > 0 {
			layers = fmt.Sprintf("%d", e.LayerCount)
		}

		digest := "-"
		if e.Digest != "" {
			digest = formatDigestShort(e.Digest)
		}

		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			e.Name, e.Target, e.Status, size, layers, digest)

		if verbose && len(e.Layers) > 0 {
			for i, l := range e.Layers {
				prefix := "├─"
				if i == len(e.Layers)-1 {
					prefix = "└─"
				}
				_, _ = fmt.Fprintf(tw, "  %s\t\t%s\t%s\t\t%s\n",
					prefix, l.Status, formatSize(l.Size), formatDigestShort(l.Digest))
			}
		}
	}

	_ = tw.Flush()
}

// formatDigestShort returns a human-friendly short digest like "sha256:a1b2c3d4e5f6".
func formatDigestShort(digest string) string {
	algo, hex, ok := strings.Cut(digest, ":")
	if !ok {
		return digest
	}
	if len(hex) > 12 {
		hex = hex[:12]
	}
	return algo + ":" + hex
}
