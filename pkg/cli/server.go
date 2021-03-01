package cli

import (
	"os"
	"strconv"
	"fmt"

	"github.com/spf13/cobra"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/replicate/modelserver/pkg/global"
	"github.com/replicate/modelserver/pkg/secrets"
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
	cmd.Flags().IntVar(&global.Port, "port", 0, "Server port")

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
	if global.CloudSQLPassword == "" {
		global.CloudSQLPassword, err = secrets.FetchSecret("projects/replicate/secrets/modelserver-db-password/versions/latest")
		if err != nil {
			return err
		}
	}
	if global.Port == 0 {
		portEnv := os.Getenv("PORT")
		if portEnv == "" {
			return fmt.Errorf("--port flag or PORT env must be defined")
		}
		global.Port, err = strconv.Atoi(portEnv)
		if err != nil {
			return fmt.Errorf("Failed to convert PORT %s to integer", portEnv)
		}
	}

	s, err := server.NewServer()
	if err != nil {
		return err
	}
	return s.Start()
}
