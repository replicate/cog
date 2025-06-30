package cli

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/api"
	"github.com/replicate/cog/pkg/coglog"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/http"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/pkg/web"
)

func newPullCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use: "pull [IMAGE] [FOLDER]",

		Short:   "Pull the contents of a model into your local directory.",
		Example: `cog pull r8.im/your-username/hotdog-detector`,
		RunE:    pull,
		Args:    cobra.MinimumNArgs(1),
		Hidden:  true,
	}
	addPipelineImage(cmd)

	return cmd
}

func extractTarFile(projectDir string) func(*tar.Header, *tar.Reader) error {
	return func(header *tar.Header, tr *tar.Reader) error {
		// gosec reports this as a security vulnerability, however we do check that the target
		// is within the project directory after resolving it to its absolute path.
		target := filepath.Join(projectDir, header.Name) // #nosec G305
		target, err := filepath.Abs(target)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(target, projectDir) {
			return errors.New("Illegal access, attempted to write to " + target)
		}

		if strings.HasPrefix(filepath.Base(target), "._") {
			return nil
		}

		switch header.Typeflag {
		case tar.TypeDir:
			console.Infof("Creating directory %s", target)
			err := os.MkdirAll(target, 0o755)
			if err != nil {
				return err
			}
		case tar.TypeReg:
			console.Infof("Creating file %s", target)
			err := os.MkdirAll(filepath.Dir(target), 0o755)
			if err != nil {
				return err
			}
			outFile, err := os.Create(target)
			if err != nil {
				return err
			}
			defer outFile.Close()

			_, err = io.Copy(outFile, tr)
			if err != nil {
				return err
			}

			err = os.Chmod(target, os.FileMode(header.Mode)) // #nosec G115
			if err != nil {
				return err
			}
		case tar.TypeSymlink:
			link := filepath.Join(projectDir, header.Linkname) // #nosec G305
			link, err := filepath.Abs(link)
			if err != nil {
				return err
			}
			if !strings.HasPrefix(link, projectDir) {
				return errors.New("Illegal access, attempted to link to " + link)
			}

			console.Infof("Creating symlink %s -> %s", target, link)

			err = os.MkdirAll(filepath.Dir(target), 0o755)
			if err != nil {
				return err
			}

			err = os.Symlink(link, target)
			if err != nil {
				return err
			}

		default:
			return fmt.Errorf("unknown file type: %v", header.Typeflag)
		}
		return nil
	}
}

func imageToDir(image string, projectDir string) string {
	imageComponents := strings.Split(image, "/")
	modelName := imageComponents[len(imageComponents)-1]
	modelNameComponents := strings.Split(modelName, ":")
	return filepath.Join(projectDir, modelNameComponents[0])
}

func pull(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Create the clients
	dockerClient, err := docker.NewClient(ctx)
	if err != nil {
		return err
	}

	client, err := http.ProvideHTTPClient(ctx, dockerClient)
	if err != nil {
		return err
	}

	logClient := coglog.NewClient(client)
	logCtx := logClient.StartPull()

	// Find image name
	projectDir, err := os.Getwd()
	if err != nil {
		logClient.EndPull(ctx, err, logCtx)
		return err
	}
	image := args[0]

	// Create the working folder
	if len(args) == 1 {
		projectDir = imageToDir(image, projectDir)
	} else {
		projectDir = filepath.Join(projectDir, args[1])
	}
	err = os.MkdirAll(projectDir, 0o755)
	if err != nil {
		logClient.EndPull(ctx, err, logCtx)
		return err
	}

	// Check if we are in a pipeline
	if !pipelinesImage {
		err = errors.New("Please use docker pull " + image + " to download this model.")
		logClient.EndPull(ctx, err, logCtx)
		return err
	}

	webClient := web.NewClient(dockerClient, client)
	apiClient := api.NewClient(dockerClient, client, webClient)

	// Pull the source
	err = apiClient.PullSource(ctx, image, extractTarFile(projectDir))
	if err != nil {
		logClient.EndPull(ctx, err, logCtx)
		return err
	}

	logClient.EndPull(ctx, nil, logCtx)
	return nil
}
