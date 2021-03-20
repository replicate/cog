package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/xeonx/timeago"

	"github.com/replicate/cog/pkg/model"
)

func newListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List Cog packages",
		RunE:  listPackages,
		Args:  cobra.NoArgs,
	}

	return cmd
}

func listPackages(cmd *cobra.Command, args []string) error {
	resp, err := http.Get(remoteHost() + "/v1/packages/")
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("List endpoint returned status %d", resp.StatusCode)
	}

	models := []*model.Model{}
	if err := json.NewDecoder(resp.Body).Decode(&models); err != nil {
		return fmt.Errorf("Failed to decode response: %w", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tCREATED")
	for _, mod := range models {
		fmt.Fprintf(w, "%s\t%s\t%s\n", mod.ID, mod.Name, timeago.English.Format(mod.Created))
	}
	w.Flush()

	return nil
}
