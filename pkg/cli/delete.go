package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/client"
)

func newDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "delete",
		Short:   "Delete a package",
		RunE:    deletePackage,
		Args:    cobra.MinimumNArgs(1),
		Aliases: []string{"rm"},
	}
	return cmd
}

func deletePackage(cmd *cobra.Command, args []string) error {
	repo, err := getRepo()
	if err != nil {
		return err
	}
	cli := client.NewClient()
	for _, id := range args {
		if err := cli.DeletePackage(repo, id); err != nil {
			return err
		}
		fmt.Printf("Deleted package %s:%s\n", repo, id)
	}
	return nil
}
