package cli

import (
	"github.com/spf13/cobra"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/replicate/modelserver/pkg/global"
	"github.com/replicate/modelserver/pkg/server"
)

func newServerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use: "server",
		Short: "Start server",
		RunE: startServer,
		Args: cobra.NoArgs,
	}

	cmd.Flags().String("access-token", "", "GCP access token (optional)")
	cmd.Flags().StringVar(&global.CloudSQLPassword, "cloud-sql-password", "", "Cloud SQL password")

	return cmd
}

func startServer(cmd *cobra.Command, args []string) error {
	accessToken, err := cmd.Flags().GetString("access-token")
	if err != nil {
		return err
	}

	if accessToken == "" {
		global.TokenSource = google.ComputeTokenSource("default")
	} else {
		token := oauth2.Token{AccessToken: accessToken}
		global.TokenSource = oauth2.StaticTokenSource(&token)
	}

	s, err := server.NewServer()
	if err != nil {
		return err
	}
	return s.Start()
}
