package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/database"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/server"
	"github.com/replicate/cog/pkg/serving"
	"github.com/replicate/cog/pkg/storage"
	"github.com/replicate/cog/pkg/util/terminal"
)

var (
	port                  int
	dockerRegistry        string
	postUploadHooks       []string
	postBuildPrimaryHooks []string
	postBuildHooks        []string
	authDelegate          string
	cpuConcurrency        int
	gpuConcurrency        int
)

func newServerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Start server",
		RunE:  startServer,
		Args:  cobra.NoArgs,
	}

	// TODO(andreas): more detailed documentation on the web hooks and their exact semantics

	cmd.Flags().IntVar(&port, "port", 8080, "Server port")
	cmd.Flags().StringVar(&dockerRegistry, "docker-registry", "", "Docker registry to push images to")
	cmd.Flags().StringArrayVar(&postUploadHooks, "post-upload-hook", []string{}, "Web hooks that are posted to after a version has been uploaded. Format: <url>@<secret>")
	cmd.Flags().StringArrayVar(&postBuildPrimaryHooks, "post-build-primary-hook", []string{}, "Web hooks that are posted to after the CPU image (or GPU image if no CPU image exists) has been built. Format: <url>@<secret>")
	cmd.Flags().StringArrayVar(&postBuildHooks, "post-build-hook", []string{}, "Web hooks that are posted to after an image has been built. Format: <url>@<secret>")
	cmd.Flags().StringVar(&authDelegate, "auth-delegate", "", "Address to service that handles authentication logic")
	cmd.Flags().IntVar(&cpuConcurrency, "cpu-concurrency", 4, "Number of concurrent CPU builds")
	cmd.Flags().IntVar(&gpuConcurrency, "gpu-concurrency", 1, "Number of concurrent GPU builds")
	return cmd
}

func startServer(cmd *cobra.Command, args []string) error {
	ui := terminal.ConsoleUI(context.Background())
	defer ui.Close()
	st := ui.Status()
	st.Update("Starting server...")

	var err error
	if port == 0 {
		portEnv := os.Getenv("PORT")
		if portEnv == "" {
			return fmt.Errorf("--port flag or PORT env must be defined")
		}
		port, err = strconv.Atoi(portEnv)
		if err != nil {
			return fmt.Errorf("PORT environment variable is not an integer: %s", portEnv)
		}
	}

	// TODO(andreas): make this configurable
	dataDir := ".cog"
	storageDir := filepath.Join(dataDir, "storage")
	if err := os.MkdirAll(storageDir, 0755); err != nil {
		return fmt.Errorf("Failed to create %s: %w", storageDir, err)
	}
	databaseDir := filepath.Join(dataDir, "database")
	if err := os.MkdirAll(databaseDir, 0755); err != nil {
		return fmt.Errorf("Failed to create %s: %w", databaseDir, err)
	}

	db, err := database.NewLocalFileDatabase(databaseDir)
	if err != nil {
		return err
	}
	if dockerRegistry == "" {
		ui.Output("Running without a Docker registry, so any Docker images won't be persisted. Pass the --docker-registry flag to persist images.\n")
	}
	dockerImageBuilder := docker.NewLocalImageBuilder(dockerRegistry)
	servingPlatform, err := serving.NewLocalDockerPlatform()
	if err != nil {
		return err
	}
	store, err := storage.NewLocalStorage(storageDir)
	if err != nil {
		return err
	}
	s, err := server.NewServer(cpuConcurrency, gpuConcurrency, postUploadHooks, postBuildHooks, postBuildPrimaryHooks, authDelegate, db, dockerImageBuilder, servingPlatform, store)
	if err != nil {
		return err
	}

	st.Step(terminal.StatusOK, fmt.Sprintf("Server running on 0.0.0.0:%d", port))
	st.Close()
	return s.Start(port)
}
