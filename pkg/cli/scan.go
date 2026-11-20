package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/registry"
	"github.com/replicate/cog/pkg/scan"
	"github.com/replicate/cog/pkg/weights"
)

// ErrScanFindings means the scan completed and found potential secrets.
var ErrScanFindings = errors.New("scan found potential secrets")

type imageOpener func(context.Context, string) (io.ReadCloser, error)

func newScanCommand() *cobra.Command {
	return newScanCommandWithOpener(nil)
}

func newScanCommandWithOpener(open imageOpener) *cobra.Command {
	return newScanCommandWithBuilder(open, nil)
}

func newScanCommandWithBuilder(open imageOpener, build func(*cobra.Command) (*model.Model, error)) *cobra.Command {
	var jsonOutput, showPaths bool
	cmd := &cobra.Command{
		Use:          "scan [IMAGE|.]",
		SilenceUsage: true,
		Short:        "Build and scan the current project or scan a local Docker image for secrets",
		Long:         "Scan every layer (including deleted files) and the image config. With no argument or with '.', build the current project first. With IMAGE, scan only that local image without building or pulling. An incomplete scan fails instead of reporting clean. Exit 0 means clean, 2 means findings, and 1 means a scan error. Paths are hashed unless --show-paths is set.",
		Example:      "  cog scan\n  cog scan . --secret id=token,src=token.txt\n  cog scan my-image:latest\n  cog scan my-image:latest --json\n  cog scan my-image:latest --show-paths",
		Args:         cobra.MaximumNArgs(1),
		PreRunE:      checkMutuallyExclusiveFlags,
		RunE: func(cmd *cobra.Command, args []string) error {
			image := ""
			if len(args) == 0 || args[0] == "." {
				builder := build
				if builder == nil {
					builder = buildScanProject
				}
				m, err := builder(cmd)
				if err != nil {
					return err
				}
				if m == nil || m.Image == nil || m.Image.Digest == "" {
					return errors.New("build did not return an immutable image ID")
				}
				image = m.Image.Digest
			} else {
				image = args[0]
			}
			opener := open
			if opener == nil {
				dockerClient, err := docker.NewClient(cmd.Context())
				if err != nil {
					return fmt.Errorf("cannot connect to Docker: %w", err)
				}
				opener = func(ctx context.Context, image string) (io.ReadCloser, error) {
					inspected, err := dockerClient.Inspect(ctx, image)
					if err != nil {
						return nil, fmt.Errorf("inspect local image (build or pull it explicitly before scanning): %w", err)
					}
					// Save the inspected image by immutable ID so a concurrent tag change cannot switch it.
					stream, err := dockerClient.ImageSave(ctx, inspected.ID)
					if err != nil {
						return nil, fmt.Errorf("could not export local image: %w", err)
					}
					return stream, nil
				}
			}
			return runScanWithPaths(cmd.Context(), image, jsonOutput, showPaths, cmd.OutOrStdout(), opener)
		},
	}
	addBuildProgressOutputFlag(cmd)
	addSecretsFlag(cmd)
	addNoCacheFlag(cmd)
	addSeparateWeightsFlag(cmd)
	addSchemaFlag(cmd)
	addUseCudaBaseImageFlag(cmd)
	addDockerfileFlag(cmd)
	addUseCogBaseImageFlag(cmd)
	addBuildTimestampFlag(cmd)
	addStripFlag(cmd)
	addPrecompileFlag(cmd)
	addConfigFlag(cmd)
	cmd.Flags().StringVar(&configFilename, "config", "cog.yaml", "The name of the config file (alias for --file)")
	cmd.MarkFlagsMutuallyExclusive("config", "file")
	addSkipSchemaValidationFlag(cmd)
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Print versioned JSON findings (without secret values)")
	cmd.Flags().BoolVar(&showPaths, "show-paths", false, "Reveal local image file paths, which may contain secrets")
	return cmd
}

// buildScanProject uses the normal build pipeline; build diagnostics go to stderr.
func buildScanProject(cmd *cobra.Command) (*model.Model, error) {
	ctx := cmd.Context()
	dockerClient, err := docker.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	src, err := model.NewSource(configFilename)
	if err != nil {
		return nil, err
	}
	defer src.Close()
	if err := weights.CheckDrift(src.ProjectDir, src.Config.Weights); err != nil {
		return nil, err
	}
	imageName := src.Config.Image
	if imageName == "" {
		imageName = src.Config.Model
	}
	if imageName == "" {
		imageName = config.DockerImageName(src.ProjectDir)
	}
	resolver := model.NewResolver(dockerClient, registry.NewRegistryClient())
	return resolver.Build(ctx, src, buildOptionsFromFlags(cmd, imageName, nil))
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
