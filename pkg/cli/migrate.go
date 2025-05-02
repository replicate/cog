package cli

import (
	"github.com/spf13/cobra"

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

	return cmd
}

func cmdMigrate(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	migrator, err := migrate.NewMigrator(migrate.MigrationV1, migrate.MigrationV1Fast, !migrateAccept)
	if err != nil {
		return err
	}
	err = migrator.Migrate(ctx)
	if err != nil {
		return err
	}

	return nil
}

func addYesFlag(cmd *cobra.Command) {
	const acceptFlag = "y"
	cmd.Flags().BoolVar(&buildPrecompile, acceptFlag, false, "Whether to disable interaction and automatically accept the changes.")
}
