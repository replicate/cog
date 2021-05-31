package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/TylerBrock/colorjson"
	"github.com/mitchellh/go-homedir"
	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/client"
	"github.com/replicate/cog/pkg/logger"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/serving"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/pkg/util/mime"
	"github.com/replicate/cog/pkg/util/slices"
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
	if !slices.ContainsString([]string{"cpu", "gpu"}, inferArch) {
		return fmt.Errorf("--arch must be either 'cpu' or 'gpu'")
	}

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
	image := model.ImageForArch(version.Images, inferArch)
	if image == nil {
		return fmt.Errorf("No %s image has been built for %s:%s", inferArch, mod.String(), id)
	}

	servingPlatform, err := serving.NewLocalDockerPlatform()
	if err != nil {
		return err
	}
	logWriter := logger.NewConsoleLogger()
	useGPU := inferArch == "gpu"
	deployment, err := servingPlatform.Deploy(context.Background(), image.URI, useGPU, logWriter)
	if err != nil {
		return err
	}
	defer func() {
		if err := deployment.Undeploy(); err != nil {
			console.Warnf("Failed to kill Docker container: %s", err)
		}
	}()

	return inferIndividualInputs(deployment, inputs, outPath, logWriter)
}

func inferIndividualInputs(deployment serving.Deployment, inputs []string, outputPath string, logWriter logger.Logger) error {
	example := parseInferInputs(inputs)
	result, err := deployment.RunInference(context.Background(), example, logWriter)
	if err != nil {
		return err
	}
	// TODO(andreas): support multiple outputs?
	output := result.Values["output"]

	// Write to stdout
	if outputPath == "" {
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
		outputPath = "output"
		extension := mime.ExtensionByType(output.MimeType)
		if extension != "" {
			outputPath += extension
		}
	}

	// Ignore @, to make it behave the same as -i
	outputPath = strings.TrimPrefix(outputPath, "@")

	outputPath, err = homedir.Expand(outputPath)
	if err != nil {
		return err
	}

	// Write to file
	outFile, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE, 0755)
	if err != nil {
		return err
	}

	if _, err := io.Copy(outFile, output.Buffer); err != nil {
		return err
	}

	fmt.Println("Written output to " + outputPath)
	return nil
}

func parseInferInputs(inputs []string) *serving.Example {
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
		if strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) {
			value = value[1 : len(value)-1]
		}
		keyVals[name] = value
	}
	return serving.NewExample(keyVals)
}
