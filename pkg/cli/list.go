package cli

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/xeonx/timeago"

	"github.com/replicate/cog/pkg/client"
)

func newListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List Cog packages",
		RunE:    listPackages,
		Args:    cobra.NoArgs,
		Aliases: []string{"ls"},
	}
	addRepoFlag(cmd)

	return cmd
}

func listPackages(cmd *cobra.Command, args []string) error {
	repo, err := getRepo()
	if err != nil {
		return err
	}

	cli := client.NewClient()
	models, err := cli.ListPackages(repo)
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tCREATED")
	for _, mod := range models {
		fmt.Fprintf(w, "%s\t%s\n", mod.ID, timeago.English.Format(mod.Created))
	}
	w.Flush()

	return nil
}
