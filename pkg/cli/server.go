package cli

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/database"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/server"
	"github.com/replicate/cog/pkg/serving"
	"github.com/replicate/cog/pkg/storage"
	"github.com/replicate/cog/pkg/util/terminal"
)

const (
	storageDir = ".cog/storage"

	// TODO(andreas): make this configurable
	localDatabaseDir = ".cog/database"
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
	serverDatabase        string
	dbHost                string
	dbPort                int
	dbUser                string
	dbPassword            string
	dbName                string
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
	cmd.Flags().StringVar(&serverDatabase, "database", "filesystem", "Database (options: 'filesystem', 'postgres', 'googlecloudsql')")

	cmd.Flags().StringVar(&dbHost, "db-host", "", "Database host (applicable when --database=postgres or --database=googlecloudsql)")
	cmd.Flags().IntVar(&dbPort, "db-port", 5432, "Database port (applicable when --database=postgres or --database=googlecloudsql)")
	cmd.Flags().StringVar(&dbUser, "db-user", "", "Database user (applicable when --database=postgres or --database=googlecloudsql)")
	cmd.Flags().StringVar(&dbPassword, "db-password", "", "Database password (applicable when --database=postgres or --database=googlecloudsql)")
	cmd.Flags().StringVar(&dbName, "db-name", "", "Database name (applicable when --database=postgres or --database=googlecloudsql)")

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

	if err := os.MkdirAll(storageDir, 0755); err != nil {
		return fmt.Errorf("Failed to create %s: %w", storageDir, err)
	}
	db, err := database.NewDatabase(serverDatabase, dbHost, dbPort, dbUser, dbPassword, dbName, localDatabaseDir)
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
