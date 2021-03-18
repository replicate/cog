package cli

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/replicate/cog/pkg/client"
	"github.com/spf13/cobra"
)

var outPath string

func newInferCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "infer <id>",
		Short: "Run a single inference against a Cog package",
		RunE:  cmdInfer,
		Args:  cobra.MinimumNArgs(1),
	}
	cmd.Flags().StringVarP(&outPath, "output", "o", "", "output path")
	return cmd
}

func cmdInfer(cmd *cobra.Command, args []string) error {
	packageId := args[0]

	serverUrl := "http://localhost:8080"
	client := client.NewClient(serverUrl)
	pkg, err := client.GetPackage(packageId)
	if err != nil {
		return err
	}

	artifact := pkg.Artifacts[0]

	fmt.Println("--> Running Docker image", artifact.URI)
	out, err := exec.Command("docker", "run", "-p", "5000:5000", "-d", artifact.URI).CombinedOutput()
	if err != nil {
		fmt.Println(string(out))
		return err
	}
	containerID := strings.TrimSpace(string(out))
	defer exec.Command("docker", "kill", containerID).CombinedOutput()

	waitForPort("localhost:5000")
	time.Sleep(3 * time.Second)

	fmt.Println("--> Running inference")

	resp, err := http.Post("http://localhost:5000/infer", "plain/text", bytes.NewBuffer([]byte{}))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("Status %d: %s", resp.StatusCode, body)
	}

	// TODO check content type so we don't barf binary data to stdout

	outFile := os.Stdout
	if outPath != "" {
		outFile, err = os.OpenFile(outPath, os.O_WRONLY|os.O_CREATE, 0755)
		if err != nil {
			return err
		}
	}

	if _, err := io.Copy(outFile, resp.Body); err != nil {
		return err
	}

	if outPath != "" {
		fmt.Println("--> Written output to " + outPath)

	}
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
