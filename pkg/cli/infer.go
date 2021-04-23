package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/TylerBrock/colorjson"
	"github.com/mitchellh/go-homedir"
	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/console"

	"github.com/replicate/cog/pkg/client"
	"github.com/replicate/cog/pkg/logger"
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
	addRepoFlag(cmd)
	cmd.Flags().StringArrayVarP(&inputs, "input", "i", []string{}, "Inputs, in the form name=value. if value is prefixed with @, then it is read from a file on disk. E.g. -i path=@image.jpg")
	cmd.Flags().StringVarP(&outPath, "output", "o", "", "Output path")

	return cmd
}

func cmdInfer(cmd *cobra.Command, args []string) error {
	repo, err := getRepo()
	if err != nil {
		return err
	}

	modelId := args[0]

	client := client.NewClient()
	fmt.Println("Loading package", modelId)
	mod, err := client.GetModel(repo, modelId)
	if err != nil {
		return err
	}

	servingPlatform, err := serving.NewLocalDockerPlatform()
	if err != nil {
		return err
	}
	logWriter := logger.NewConsoleLogger()
	// TODO(andreas): GPU inference
	artifact, ok := mod.ArtifactFor(model.TargetDockerCPU)
	if !ok {
		return fmt.Errorf("Target %s is not defined for model", model.TargetDockerCPU)
	}
	deployment, err := servingPlatform.Deploy(artifact.URI, logWriter)
	if err != nil {
		return err
	}
	defer func() {
		if err := deployment.Undeploy(); err != nil {
			console.Warnf("Failed to kill Docker container: %s", err)
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

	// Write to stdout
	if outPath == "" {
		// Is it something we can sensibly write to stdout?
		if output.MimeType == "plain/text" {
			_, err := io.Copy(os.Stdout, output.Buffer)
			return err
		} else if output.MimeType == "application/json" {
			var obj map[string]interface{}
			dec := json.NewDecoder(output.Buffer)
			if err := dec.Decode(&obj); err != nil {
				return err
			}
			f := colorjson.NewFormatter()
			f.Indent = 2
			s, _ := f.Marshal(obj)
			fmt.Println(string(s))
			return nil
		}
		// Otherwise, fall back to writing file
		outPath = "output"
		extension, _ := mime.ExtensionsByType(output.MimeType)
		if len(extension) > 0 {
			outPath += extension[0]
		}
	}

	// Ignore @, to make it behave the same as -i
	if strings.HasPrefix(outPath, "@") {
		outPath = outPath[1:]
	}

	outPath, err := homedir.Expand(outPath)
	if err != nil {
		return err
	}

	// Write to file
	outFile, err := os.OpenFile(outPath, os.O_WRONLY|os.O_CREATE, 0755)
	if err != nil {
		return err
	}

	if _, err := io.Copy(outFile, output.Buffer); err != nil {
		return err
	}

	fmt.Println("Written output to " + outPath)
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
