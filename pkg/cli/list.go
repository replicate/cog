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
		Short:   "List versions",
		RunE:    list,
		Args:    cobra.NoArgs,
		Aliases: []string{"ls"},
	}
	addRepoFlag(cmd)

	cmd.Flags().BoolP("quiet", "q", false, "Quite output, only display IDs")

	return cmd
}

func list(cmd *cobra.Command, args []string) error {
	repo, err := getRepo()
	if err != nil {
		return err
	}
	quiet, err := cmd.Flags().GetBool("quiet")
	if err != nil {
		return err
	}

	cli := client.NewClient()
	versions, err := cli.ListVersions(repo)
	if err != nil {
		return err
	}

	sort.Slice(versions, func(i, j int) bool {
		return versions[i].Created.After(versions[j].Created)
	})

	if quiet {
		for _, version := range versions {
			fmt.Println(version.ID)
		}
	} else {
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tCREATED")
		for _, version := range versions {
			fmt.Fprintf(w, "%s\t%s\n", version.ID, timeago.English.Format(version.Created))
		}
		w.Flush()
	}

	return nil
}
