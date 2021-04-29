package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/console"
	"github.com/replicate/cog/pkg/database"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/server"
	"github.com/replicate/cog/pkg/serving"
	"github.com/replicate/cog/pkg/storage"
)

var (
	port           int
	dockerRegistry string
	buildWebHooks  []string
	authDelegate   string
)

func newServerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Start server",
		RunE:  startServer,
		Args:  cobra.NoArgs,
	}

	cmd.Flags().IntVar(&port, "port", 0, "Server port")
	cmd.Flags().StringVar(&dockerRegistry, "docker-registry", "", "Docker registry to push images to")
	cmd.Flags().StringArrayVar(&buildWebHooks, "web-hook", []string{}, "Web hooks that are posted to after build. Format: <url>@<secret>")
	cmd.Flags().StringVar(&authDelegate, "auth-delegate", "", "Address to service that handles authentication logic")
	return cmd
}

func startServer(cmd *cobra.Command, args []string) error {
	var err error
	if port == 0 {
		portEnv := os.Getenv("PORT")
		if portEnv == "" {
			return fmt.Errorf("--port flag or PORT env must be defined")
		}
		port, err = strconv.Atoi(portEnv)
		if err != nil {
			return fmt.Errorf("Failed to convert PORT %s to integer", portEnv)
		}
	}

	console.Debugf("Preparing to start server on port %d", port)

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
		console.Warn("Running without docker registry. Please add --docker-registry to be able to push images")
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
	s, err := server.NewServer(port, buildWebHooks, authDelegate, db, dockerImageBuilder, servingPlatform, store)
	if err != nil {
		return err
	}
	return s.Start()
}
