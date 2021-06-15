package main

import (
	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/database"
	"github.com/replicate/cog/pkg/util/console"
)

const localDBDir = ".cog/database"

var dryRun bool

type dbFlags struct {
	database string
	dir      string
	host     string
	port     int
	password string
	user     string
	name     string
}

var dbFlagsMap = map[string]*dbFlags{
	"source":      new(dbFlags),
	"destination": new(dbFlags),
}

func main() {
	cmd := cobra.Command{
		Use:           "migrate",
		RunE:          migrate,
		SilenceErrors: true,
	}
	cmd.Flags().BoolVar(&dryRun, "dry", false, "Dry run")
	for direction, flags := range dbFlagsMap {
		cmd.Flags().StringVar(&flags.database, direction+"-database", "filesystem", "Database (options: 'filesystem', 'postgres', 'googlecloudsql')")

		cmd.Flags().StringVar(&flags.dir, direction+"-directory", "", "Database directory (applicable when database=filesystem)")

		cmd.Flags().StringVar(&flags.host, direction+"-host", "", "Database host (applicable when database=postgres or database=googlecloudsql)")
		cmd.Flags().IntVar(&flags.port, direction+"-port", 5432, "Database port (applicable when database=postgres or database=googlecloudsql)")
		cmd.Flags().StringVar(&flags.user, direction+"-user", "", "Database user (applicable when database=postgres or database=googlecloudsql)")
		cmd.Flags().StringVar(&flags.password, direction+"-password", "", "Database password (applicable when database=postgres or database=googlecloudsql)")
		cmd.Flags().StringVar(&flags.name, direction+"-name", "", "Database name (applicable when database=postgres or database=googlecloudsql)")
	}
	if err := cmd.Execute(); err != nil {
		console.Fatal(err.Error())
	}
}

func migrate(cmd *cobra.Command, args []string) error {
	dbMap := map[string]database.Database{}
	for direction, flags := range dbFlagsMap {
		var err error
		dbMap[direction], err = database.NewDatabase(flags.database, flags.host, flags.port, flags.user, flags.password, flags.name, flags.dir)
		if err != nil {
			return err
		}
	}
	return database.Migrate(dbMap["source"], dbMap["destination"], dryRun)
}
