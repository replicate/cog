package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/replicate/modelserver/pkg/database"
	"github.com/replicate/modelserver/pkg/docker"
	"github.com/replicate/modelserver/pkg/global"
	"github.com/replicate/modelserver/pkg/server"
	"github.com/replicate/modelserver/pkg/serving"
	"github.com/replicate/modelserver/pkg/storage"
)

func newServerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Start server",
		RunE:  startServer,
		Args:  cobra.NoArgs,
	}

	cmd.Flags().IntVar(&global.Port, "port", 0, "Server port")

	return cmd
}

func startServer(cmd *cobra.Command, args []string) error {
	var err error
	if global.Port == 0 {
		portEnv := os.Getenv("PORT")
		if portEnv == "" {
			return fmt.Errorf("--port flag or PORT env must be defined")
		}
		global.Port, err = strconv.Atoi(portEnv)
		if err != nil {
			return fmt.Errorf("Failed to convert PORT %s to integer", portEnv)
		}
	}

	log.Debugf("Preparing to start server on port %d", global.Port)

	// TODO(andreas): make this configurable
	dataDir := ".modelserver"
	storageDir := filepath.Join(dataDir, "storage")
	if err := os.MkdirAll(storageDir, 0755); err != nil {
		return fmt.Errorf("Failed to create %s: %w", storageDir, err)
	}
	databaseDir := filepath.Join(dataDir, "database")
	if err := os.MkdirAll(databaseDir, 0755); err != nil {
		return fmt.Errorf("Failed to create %s: %w", databaseDir, err)
	}
	registry := "us-central1-docker.pkg.dev/replicate/andreas-scratch"

	db, err := database.NewLocalFileDatabase(databaseDir)
	if err != nil {
		return err
	}
	dockerImageBuilder := docker.NewLocalImageBuilder(registry)
	servingPlatform, err := serving.NewLocalDockerPlatform()
	if err != nil {
		return err
	}
	store, err := storage.NewLocalStorage(storageDir)
	if err != nil {
		return err
	}
	s := server.NewServer(db, dockerImageBuilder, servingPlatform, store)
	return s.Start()
}
