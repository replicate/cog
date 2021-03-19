package cli

import (
	"io"
	"net/http"

	"fmt"

	"github.com/spf13/cobra"
)

func newDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <id> [<id2>, <id3>, ...]",
		Short: "",
		RunE:  Delete,
		Args:  cobra.MinimumNArgs(1),
	}

	cmd.Flags().StringVarP(&buildHost, "build-host", "H", "127.0.0.1:8080", "address to the build host")

	return cmd
}

func Delete(cmd *cobra.Command, args []string) error {
	// TODO(andreas): prompt yes/no

	client := new(http.Client)
	for _, id := range args {
		url := "http://"+buildHost+"/v1/packages/"+id
		req, err := http.NewRequest("DELETE", url, nil)
		if err != nil {
			return fmt.Errorf("Failed to make HTTP DELETE request: %w", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("HTTP DELETE request failed: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("HTTP DELETE request returned code %d", resp.StatusCode)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("Failed to read response body: %w", err)
		}
		fmt.Println(string(body))
	}

	return nil
}
