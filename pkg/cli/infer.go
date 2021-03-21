package cli

import (
	"fmt"
	"io"
	"io/ioutil"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/schollz/progressbar/v3"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/client"
)

var (
	inputs  []string
	outPath string
)

func newInferCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "infer <id>",
		Short: "Run a single inference against a Cog package",
		RunE:  cmdInfer,
		Args:  cobra.MinimumNArgs(1),
	}
	cmd.Flags().StringArrayVarP(&inputs, "input", "i", []string{}, "Inputs, in the form name=value. if value is prefixed with @, then it is read from a file on disk. E.g. -i path=@image.jpg")
	cmd.Flags().StringVarP(&outPath, "output", "o", "", "Output path")
	return cmd
}

func cmdInfer(cmd *cobra.Command, args []string) error {
	packageId := args[0]

	client := client.NewClient(remoteHost())
	fmt.Println("--> Loading package", packageId)
	pkg, err := client.GetPackage(packageId)
	if err != nil {
		return err
	}

	artifact := pkg.Artifacts[0]

	fmt.Println("--> Pulling and running Docker image", artifact.URI)
	out, err := exec.Command("docker", "run", "-p", "5000:5000", "-d", artifact.URI).CombinedOutput()
	if err != nil {
		fmt.Println(string(out))
		return err
	}
	containerID := strings.TrimSpace(string(out))
	defer exec.Command("docker", "kill", containerID).CombinedOutput()

	fmt.Println("--> Waiting for model to load")
	// TODO timeout
	waitForHTTP("http://localhost:5000")

	fmt.Println("--> Running inference")

	bodyReader, bodyWriter := io.Pipe()
	httpClient := &http.Client{}
	req, err := http.NewRequest(http.MethodPost, "http://localhost:5000/infer", bodyReader)
	if err != nil {
		return err
	}
	bar := progressbar.DefaultBytes(
		-1,
		"uploading",
	)

	go func() {
		// TODO: check input files exist before loading model
		err := writeInputs(req, inputs, io.MultiWriter(bodyWriter, bar))
		if err != nil {
			log.Fatal(err)
		}
		// MultiWriter is a Writer not a WriteCloser, but zipWriter is a WriteCloser, so we need to Close it ourselves
		if err = bodyWriter.Close(); err != nil {
			log.Fatal(err)
		}
	}()

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("Status %d: %s", resp.StatusCode, body)
	}

	// TODO check content type so we don't barf binary data to stdout

	contentType := resp.Header.Get("Content-Type")
	mimeType := strings.Split(contentType, ";")[0]

	if mimeType != "plain/text" && outPath == "" {
		outPath = "output"
		extension, _ := mime.ExtensionsByType(mimeType)
		if len(extension) > 0 {
			outPath += extension[0]
		}
	}

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

func waitForHTTP(url string) {
	client := http.Client{
		Timeout: 1 * time.Second,
	}
	for {
		resp, err := client.Get(url + "/ping")
		if err == nil && resp.StatusCode == http.StatusOK {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func writeInputs(req *http.Request, inputs []string, body io.Writer) error {
	mwriter := multipart.NewWriter(body)
	req.Header.Add("Content-Type", mwriter.FormDataContentType())
	defer mwriter.Close()

	for _, input := range inputs {
		var name, value string

		// Default input name is "input"
		if !strings.Contains(input, "=") {
			name = "input"
			value = input
		} else {
			split := strings.SplitN(input, "=", 2)
			name = split[0]
			value = split[1]
		}

		// Treat inputs prefixed with @ as a path to a file
		if strings.HasPrefix(value, "@") {
			filepath := value[1:]
			w, err := mwriter.CreateFormFile(name, path.Base(filepath))
			if err != nil {
				return err
			}

			file, err := os.Open(filepath)
			if err != nil {
				return err
			}
			if _, err := io.Copy(w, file); err != nil {
				return err
			}
		} else {
			w, err := mwriter.CreateFormField(name)
			if err != nil {
				return err
			}
			if _, err = w.Write([]byte(value)); err != nil {
				return err
			}
		}
	}

	// Call before defer to capture error
	return mwriter.Close()
}
