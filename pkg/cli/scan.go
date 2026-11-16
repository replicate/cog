package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/docker/docker/client"
	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/scan"
)

// ErrScanFindings means the scan completed and found potential secrets.
var ErrScanFindings = errors.New("scan found potential secrets")

type imageOpener func(context.Context, string) (io.ReadCloser, error)

func newScanCommand() *cobra.Command {
	return newScanCommandWithOpener(nil)
}

func newScanCommandWithOpener(open imageOpener) *cobra.Command {
	var jsonOutput, showPaths bool
	cmd := &cobra.Command{
		Use: "scan IMAGE", Short: "Scan an existing local Docker image for secrets",
		Long:    "Scan every layer (including deleted files) and the image config. Never builds or pulls an image. An incomplete scan fails instead of reporting clean. Exit 0 means clean, 2 means findings, and 1 means a scan error. Paths are hashed unless --show-paths is set.",
		Example: "  cog scan my-image:latest\n  cog scan my-image:latest --json\n  cog scan my-image:latest --show-paths",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opener := open
			if opener == nil {
				docker, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
				if err != nil {
					return errors.New("cannot connect to Docker")
				}
				defer docker.Close()
				opener = func(ctx context.Context, image string) (io.ReadCloser, error) {
					inspected, err := docker.ImageInspect(ctx, image)
					if err != nil {
						return nil, errors.New("image not found locally or Docker is unavailable (build or pull it explicitly before scanning)")
					}
					// Save the inspected image by immutable ID so a concurrent tag change cannot switch it.
					stream, err := docker.ImageSave(ctx, []string{inspected.ID})
					if err != nil {
						return nil, errors.New("could not export local image")
					}
					return stream, nil
				}
			}
			return runScanWithPaths(cmd.Context(), args[0], jsonOutput, showPaths, cmd.OutOrStdout(), opener)
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Print versioned JSON findings (without secret values)")
	cmd.Flags().BoolVar(&showPaths, "show-paths", false, "Reveal local image file paths, which may contain secrets")
	return cmd
}

func runScan(ctx context.Context, image string, jsonOutput bool, out io.Writer, open imageOpener) error {
	return runScanWithPaths(ctx, image, jsonOutput, false, out, open)
}

func runScanWithPaths(ctx context.Context, image string, jsonOutput, showPaths bool, out io.Writer, open imageOpener) error {
	stream, err := open(ctx, image)
	if err != nil {
		return err
	}
	defer stream.Close()
	findings, err := scan.Archive(stream)
	if err != nil {
		return err
	}
	if showPaths {
		for i := range findings {
			if findings[i].SourcePath != "" {
				findings[i].Path = findings[i].SourcePath
			}
		}
	}
	if jsonOutput {
		if findings == nil {
			findings = []scan.Finding{}
		}
		if err := json.NewEncoder(out).Encode(struct {
			Version  int            `json:"version"`
			Findings []scan.Finding `json:"findings"`
		}{1, findings}); err != nil {
			return fmt.Errorf("write scan result: %w", err)
		}
	} else {
		for _, f := range findings {
			if _, err := fmt.Fprintf(out, "%s: %s (line %d)\n", f.Path, f.Rule, f.Line); err != nil {
				return fmt.Errorf("write scan result: %w", err)
			}
		}
		if len(findings) == 0 {
			if _, err := fmt.Fprintln(out, "No potential secrets found."); err != nil {
				return fmt.Errorf("write scan result: %w", err)
			}
		}
	}
	if len(findings) > 0 {
		return ErrScanFindings
	}
	return nil
}
