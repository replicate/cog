package cli

import (
	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/coglog"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/http"
	"github.com/replicate/cog/pkg/migrate"
)

var migrateAccept bool

func newMigrateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Run a migration",
		Long: `Run a migration.

This will attempt to migrate your cog project to be compatible with fast boots.`,
		RunE:   cmdMigrate,
		Args:   cobra.MaximumNArgs(0),
		Hidden: true,
	}

	addYesFlag(cmd)
	addConfigFlag(cmd)

	return cmd
}

func cmdMigrate(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	dockerClient, err := docker.NewClient(ctx)
	if err != nil {
		return err
	}

	client, err := http.ProvideHTTPClient(ctx, dockerClient)
	if err != nil {
		return err
	}
	logClient := coglog.NewClient(client)
	logCtx := logClient.StartMigrate(migrateAccept)

	migrator, err := migrate.NewMigrator(migrate.MigrationV1, migrate.MigrationV1Fast, !migrateAccept, logCtx)
	if err != nil {
		logClient.EndMigrate(ctx, err, logCtx)
		return err
	}
	err = migrator.Migrate(ctx, configFilename)
	if err != nil {
		logClient.EndMigrate(ctx, err, logCtx)
		return err
	}
	logClient.EndMigrate(ctx, nil, logCtx)

	return nil
}

func addYesFlag(cmd *cobra.Command) {
	const acceptFlag = "y"
	cmd.Flags().BoolVar(&migrateAccept, acceptFlag, false, "Whether to disable interaction and automatically accept the changes.")
}
