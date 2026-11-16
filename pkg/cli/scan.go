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

var errScanFindings = errors.New("scan found potential secrets")

type imageOpener func(context.Context, string) (io.ReadCloser, error)

func newScanCommand() *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use: "scan IMAGE", Short: "Scan an existing local Docker image for secrets",
		Long:    "Scan every layer (including deleted files) and the image config. Never builds or pulls an image. An incomplete scan fails instead of reporting clean.",
		Example: "  cog scan my-image:latest\n  cog scan my-image:latest --json",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			docker, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
			if err != nil {
				return errors.New("cannot connect to Docker")
			}
			defer docker.Close()
			opener := func(ctx context.Context, image string) (io.ReadCloser, error) {
				inspected, err := docker.ImageInspect(ctx, image)
				if err != nil {
					return nil, errors.New("image not found locally or Docker is unavailable (build or pull it explicitly before scanning)")
				}
				// Save the inspected image by immutable ID so a concurrent tag change
				// cannot silently switch the image under the scanner.
				stream, err := docker.ImageSave(ctx, []string{inspected.ID})
				if err != nil {
					return nil, errors.New("could not export local image")
				}
				return stream, nil
			}
			out := cmd.ErrOrStderr()
			if jsonOutput {
				out = cmd.OutOrStdout()
			}
			return runScan(cmd.Context(), args[0], jsonOutput, out, opener)
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Print versioned JSON findings (without secret values)")
	return cmd
}

func runScan(ctx context.Context, image string, jsonOutput bool, out io.Writer, open imageOpener) error {
	stream, err := open(ctx, image)
	if err != nil {
		return err
	}
	defer stream.Close()
	findings, err := scan.Archive(stream)
	if err != nil {
		return err
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
		return errScanFindings
	}
	return nil
}
