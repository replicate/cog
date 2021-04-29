package cli

import (
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/xeonx/timeago"

	"github.com/replicate/cog/pkg/client"
)

func newListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List Cog models",
		RunE:    listModels,
		Args:    cobra.NoArgs,
		Aliases: []string{"ls"},
	}
	addRepoFlag(cmd)

	cmd.Flags().BoolP("quiet", "q", false, "Quite output, only display IDs")

	return cmd
}

func listModels(cmd *cobra.Command, args []string) error {
	repo, err := getRepo()
	if err != nil {
		return err
	}
	quiet, err := cmd.Flags().GetBool("quiet")
	if err != nil {
		return err
	}

	cli := client.NewClient()
	models, err := cli.ListModels(repo)
	if err != nil {
		return err
	}

	sort.Slice(models, func(i, j int) bool {
		return models[i].Created.After(models[j].Created)
	})

	if quiet {
		for _, mod := range models {
			fmt.Println(mod.ID)
		}
	} else {
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tCREATED")
		for _, mod := range models {
			fmt.Fprintf(w, "%s\t%s\n", mod.ID, timeago.English.Format(mod.Created))
		}
		w.Flush()
	}

	return nil
}
