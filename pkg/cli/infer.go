package cli

import (
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/client"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/serving"
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

	servingPlatform, err := serving.NewLocalDockerPlatform()
	if err != nil {
		return err
	}
	logWriter := func(s string) { log.Debug(s) }
	deployment, err := servingPlatform.Deploy(pkg, model.TargetDockerCPU, logWriter)
	if err != nil {
		return err
	}
	defer func() {
		if err := deployment.Undeploy(); err != nil {
			log.Warnf("Failed to kill Docker container: %s", err)
		}
	}()

	keyVals := map[string]string{}
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
		keyVals[name] = value
	}
	example := serving.NewExample(keyVals)
	result, err := deployment.RunInference(example, logWriter)
	if err != nil {
		return err
	}
	// TODO(andreas): support multiple outputs?
	output := result.Values["output"]

	// TODO check content type so we don't barf binary data to stdout

	if output.MimeType != "plain/text" && outPath == "" {
		outPath = "output"
		extension, _ := mime.ExtensionsByType(output.MimeType)
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

	if _, err := io.Copy(outFile, output.Buffer); err != nil {
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
