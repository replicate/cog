package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/mitchellh/go-homedir"
	"github.com/spf13/cobra"
	"github.com/vincent-petithory/dataurl"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/image"
	"github.com/replicate/cog/pkg/predict"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/pkg/util/mime"
)

var (
	inputFlags []string
	outPath    string
)

func newPredictCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "predict [image]",
		Short: "Run a prediction",
		Long: `Run a prediction.

If 'image' is passed, it will run the prediction on that Docker image.
It must be an image that has been built by Cog.

Otherwise, it will build the model in the current directory and run
the prediction on that.`,
		RunE:       cmdPredict,
		Args:       cobra.MaximumNArgs(1),
		SuggestFor: []string{"infer"},
	}
	addBuildProgressOutputFlag(cmd)
	cmd.Flags().StringArrayVarP(&inputFlags, "input", "i", []string{}, "Inputs, in the form name=value. if value is prefixed with @, then it is read from a file on disk. E.g. -i path=@image.jpg")
	cmd.Flags().StringVarP(&outPath, "output", "o", "", "Output path")

	return cmd
}

func cmdPredict(cmd *cobra.Command, args []string) error {
	imageName := ""
	volumes := []docker.Volume{}
	gpus := ""

	if len(args) == 0 {
		// Build image

		cfg, projectDir, err := config.GetConfig(projectDirFlag)
		if err != nil {
			return err
		}

		if imageName, err = image.BuildBase(cfg, projectDir, buildProgressOutput); err != nil {
			return err
		}

		// Base image doesn't have /src in it, so mount as volume
		volumes = append(volumes, docker.Volume{
			Source:      projectDir,
			Destination: "/src",
		})

		if cfg.Build.GPU {
			gpus = "all"
		}

	} else {
		// Use existing image
		imageName = args[0]

		exists, err := docker.ImageExists(imageName)
		if err != nil {
			return fmt.Errorf("Failed to determine if %s exists: %w", imageName, err)
		}
		if !exists {
			console.Infof("Pulling image: %s", imageName)
			if err := docker.Pull(imageName); err != nil {
				return fmt.Errorf("Failed to pull %s: %w", imageName, err)
			}
		}
		conf, err := image.GetConfig(imageName)
		if err != nil {
			return err
		}
		if conf.Build.GPU {
			gpus = "all"
		}
	}

	console.Info("")
	console.Infof("Starting Docker image %s and running setup()...", imageName)

	predictor := predict.NewPredictor(docker.RunOptions{
		GPUs:    gpus,
		Image:   imageName,
		Volumes: volumes,
	}, "predictions")

	go func() {
		captureSignal := make(chan os.Signal, 1)
		signal.Notify(captureSignal, syscall.SIGINT)

		<-captureSignal

		console.Info("Stopping container...")
		if err := predictor.Stop(); err != nil {
			console.Warnf("Failed to stop container: %s", err)
		}
	}()

	if err := predictor.Start(os.Stderr); err != nil {
		return err
	}

	// FIXME: will not run on signal
	defer func() {
		console.Debugf("Stopping container...")
		if err := predictor.Stop(); err != nil {
			console.Warnf("Failed to stop container: %s", err)
		}
	}()

	return predictIndividualInputs(predictor, inputFlags, outPath)
}

func predictIndividualInputs(predictor predict.Predictor, inputFlags []string, outputPath string) error {
	console.Info("Running prediction...")
	schema, err := predictor.GetSchema()
	if err != nil {
		return err
	}

	inputs, err := parseInputFlags(inputFlags, schema)
	if err != nil {
		return err
	}
	prediction, err := predictor.Predict(inputs)
	if err != nil {
		return err
	}

	// Generate output depending on type in schema
	var out []byte
	responseSchema := schema.Paths["/predictions"].Post.Responses["200"].Value.Content["application/json"].Schema.Value
	outputSchema := responseSchema.Properties["output"].Value

	// Multiple outputs!
	if outputSchema.Type == "array" && outputSchema.Items.Value != nil && outputSchema.Items.Value.Type == "string" && outputSchema.Items.Value.Format == "uri" {
		return handleMultipleFileOutput(prediction, outputSchema)
	}

	if outputSchema.Type == "string" && outputSchema.Format == "uri" {
		dataurlObj, err := dataurl.DecodeString((*prediction.Output).(string))
		if err != nil {
			return fmt.Errorf("Failed to decode dataurl: %w", err)
		}
		out = dataurlObj.Data
		if outputPath == "" {
			outputPath = "output"
			extension := mime.ExtensionByType(dataurlObj.ContentType())
			if extension != "" {
				outputPath += extension
			}
		}
	} else if outputSchema.Type == "string" {
		// Handle strings separately because if we encode it to JSON it will be surrounded by quotes.
		s := (*prediction.Output).(string)
		out = []byte(s)
	} else {
		// Treat everything else as JSON -- ints, floats, bools will all convert correctly.
		rawJSON, err := json.Marshal(prediction.Output)
		if err != nil {
			return fmt.Errorf("Failed to encode prediction output as JSON: %w", err)
		}
		var indentedJSON bytes.Buffer
		if err := json.Indent(&indentedJSON, rawJSON, "", "  "); err != nil {
			return err
		}
		out = indentedJSON.Bytes()

		// FIXME: this stopped working
		// f := colorjson.NewFormatter()
		// f.Indent = 2
		// s, _ := f.Marshal(obj)

	}

	// Write to stdout
	if outputPath == "" {
		console.Output(string(out))
		return nil
	}

	// Fall back to writing file

	// Ignore @, to make it behave the same as -i
	outputPath = strings.TrimPrefix(outputPath, "@")

	return writeOutput(outputPath, out)
}

func writeOutput(outputPath string, output []byte) error {
	outputPath, err := homedir.Expand(outputPath)
	if err != nil {
		return err
	}

	// Write to file
	outFile, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE, 0o755)
	if err != nil {
		return err
	}

	if _, err := outFile.Write(output); err != nil {
		return err
	}
	if err := outFile.Close(); err != nil {
		return err
	}
	console.Infof("Written output to %s", outputPath)
	return nil
}

func handleMultipleFileOutput(prediction *predict.Response, outputSchema *openapi3.Schema) error {
	outputs, ok := (*prediction.Output).([]interface{})
	if !ok {
		return fmt.Errorf("Failed to decode output")
	}

	for i, output := range outputs {
		outputString := output.(string)
		dataurlObj, err := dataurl.DecodeString(outputString)
		if err != nil {
			return fmt.Errorf("Failed to decode dataurl: %w", err)
		}
		out := dataurlObj.Data
		extension := mime.ExtensionByType(dataurlObj.ContentType())
		outputPath := fmt.Sprintf("output.%d%s", i, extension)
		if err := writeOutput(outputPath, out); err != nil {
			return err
		}
	}

	return nil
}

func parseInputFlags(inputs []string, schema *openapi3.T) (predict.Inputs, error) {
	var err error
	keyVals := map[string]string{}
	for _, input := range inputs {
		var name, value string

		// Default input name is "input"
		if !strings.Contains(input, "=") {
			name, err = getFirstInput(schema)
			if err != nil {
				return nil, err
			}
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
	return predict.NewInputs(keyVals), nil
}

func getFirstInput(schema *openapi3.T) (string, error) {
	inputProperties := schema.Components.Schemas["Input"].Value.Properties
	for k, v := range inputProperties {
		val, ok := v.Value.Extensions["x-order"]
		if !ok {
			continue
		}
		rawMsg, ok := val.(json.RawMessage)
		if !ok {
			continue
		}
		var order int
		if err := json.Unmarshal(rawMsg, &order); err != nil {
			return "", err
		}
		if order == 0 {
			return k, nil
		}
	}
	return "", fmt.Errorf("Could not determine the default input based on the order of the inputs. Please specify inputs in the format '-i name=value'")
}
