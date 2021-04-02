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
	serverPort           int
	serverDockerRegistry string
	serverAdapters       []string
)

func newServerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Start server",
		RunE:  startServer,
		Args:  cobra.NoArgs,
	}

	cmd.Flags().IntVar(&serverPort, "port", 0, "Server port")
	cmd.Flags().StringVar(&serverDockerRegistry, "docker-registry", "", "Docker registry to push images to")
	cmd.Flags().StringArrayVar(&serverAdapters, "adapter", []string{}, "Build adapters. Each argument corresponds to a directory with a cog-adapter.yaml file")
	return cmd
}

func startServer(cmd *cobra.Command, args []string) error {
	var err error
	if serverPort == 0 {
		portEnv := os.Getenv("PORT")
		if portEnv == "" {
			return fmt.Errorf("--port flag or PORT env must be defined")
		}
		serverPort, err = strconv.Atoi(portEnv)
		if err != nil {
			return fmt.Errorf("Failed to convert PORT %s to integer", portEnv)
		}
	}

	console.Debug("Preparing to start server on port %d", serverPort)

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
	if serverDockerRegistry == "" {
		console.Warn("Running without docker registry. Please add --docker-registry to be able to push images")
	}
	dockerImageBuilder := docker.NewLocalImageBuilder(serverDockerRegistry)
	servingPlatform, err := serving.NewLocalDockerPlatform()
	if err != nil {
		return err
	}
	store, err := storage.NewLocalStorage(storageDir)
	if err != nil {
		return err
	}
	s, err := server.NewServer(serverPort, serverAdapters, serverDockerRegistry, db, dockerImageBuilder, servingPlatform, store)
	if err != nil {
		return err
	}
	return s.Start()
}
