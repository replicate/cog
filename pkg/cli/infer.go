package cli

import (
	"fmt"
	"net"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var output string

func newInferCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "infer <id>",
		Short: "Run a single inference against a Cog package",
		RunE:  runInference,
		Args:  cobra.MinimumNArgs(1),
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "output path")
	return cmd
}

func runInference(cmd *cobra.Command, args []string) error {
	fmt.Println("--> Booting model")
	out, err := exec.Command("docker", "run", "-p", "5000:5000", "-d", "us-central1-docker.pkg.dev/replicate/andreas-scratch/"+args[0]).Output()
	if err != nil {
		return err
	}
	containerID := strings.TrimSpace(string(out))
	defer exec.Command("docker", "kill", containerID).Output()

	waitForPort("localhost:5000")
	time.Sleep(3 * time.Second)

	fmt.Println("--> Running inference")
	out, err = exec.Command("curl", "--output", output, "-X", "POST", "localhost:5000/infer").Output()
	if err != nil {
		fmt.Println(out)
		return err
	}

	fmt.Println("--> Written output to " + output)
	return nil
}

func waitForPort(host string) {
	timeout := time.Duration(1) * time.Second
	for {
		conn, _ := net.DialTimeout("tcp", host, timeout)
		if conn != nil {
			conn.Close()
			break
		}
	}
	return
}
