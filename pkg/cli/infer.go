package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"os"
	"strings"

	"github.com/TylerBrock/colorjson"
	"github.com/mitchellh/go-homedir"
	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/util/console"

	"github.com/replicate/cog/pkg/client"
	"github.com/replicate/cog/pkg/logger"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/serving"
)

var (
	inputs    []string
	outPath   string
	inferArch string
)

func newInferCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "infer <id>",
		Short: "Run a single inference against a version of a model",
		RunE:  cmdInfer,
		Args:  cobra.MinimumNArgs(1),
	}
	addModelFlag(cmd)
	cmd.Flags().StringArrayVarP(&inputs, "input", "i", []string{}, "Inputs, in the form name=value. if value is prefixed with @, then it is read from a file on disk. E.g. -i path=@image.jpg")
	cmd.Flags().StringVarP(&outPath, "output", "o", "", "Output path")
	cmd.Flags().StringVarP(&inferArch, "arch", "a", "cpu", "Architecture to run inference on (cpu/gpu)")

	return cmd
}

func cmdInfer(cmd *cobra.Command, args []string) error {
	mod, err := getModel()
	if err != nil {
		return err
	}

	id := args[0]

	client := client.NewClient()
	fmt.Println("Loading package", id)
	version, err := client.GetVersion(mod, id)
	if err != nil {
		return err
	}
	// TODO(bfirsh): differentiate between failed builds and in-progress builds, and probably block here if there is an in-progress build
	image := model.ImageForArch(version.Images, benchmarkArch)
	if image == nil {
		return fmt.Errorf("No %s image has been built for %s:%s", benchmarkArch, mod.String(), id)
	}

	servingPlatform, err := serving.NewLocalDockerPlatform()
	if err != nil {
		return err
	}
	logWriter := logger.NewConsoleLogger()
	// TODO(andreas): GPU inference
	useGPU := false
	deployment, err := servingPlatform.Deploy(context.Background(), image.URI, useGPU, logWriter)
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
	result, err := deployment.RunInference(context.Background(), example, logWriter)
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
	outPath = strings.TrimPrefix(outPath, "@")

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
