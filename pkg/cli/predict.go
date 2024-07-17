package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/mitchellh/go-homedir"
	"github.com/spf13/cobra"
	"github.com/vincent-petithory/dataurl"
	"golang.org/x/sys/unix"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/image"
	"github.com/replicate/cog/pkg/predict"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/pkg/util/mime"
)

var (
	envFlags   []string
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

	addUseCudaBaseImageFlag(cmd)
	addUseCogBaseImageFlag(cmd)
	addBuildProgressOutputFlag(cmd)
	addDockerfileFlag(cmd)
	addGpusFlag(cmd)

	cmd.Flags().StringArrayVarP(&inputFlags, "input", "i", []string{}, "Inputs, in the form name=value. if value is prefixed with @, then it is read from a file on disk. E.g. -i path=@image.jpg")
	cmd.Flags().StringVarP(&outPath, "output", "o", "", "Output path")
	cmd.Flags().StringArrayVarP(&envFlags, "env", "e", []string{}, "Environment variables, in the form name=value")

	return cmd
}

func cmdPredict(cmd *cobra.Command, args []string) error {
	imageName := ""
	volumes := []docker.Volume{}
	gpus := gpusFlag

	if len(args) == 0 {
		// Build image

		cfg, projectDir, err := config.GetConfig(projectDirFlag)
		if err != nil {
			return err
		}

		if imageName, err = image.BuildBase(cfg, projectDir, buildUseCudaBaseImage, buildUseCogBaseImage, buildProgressOutput); err != nil {
			return err
		}

		// Base image doesn't have /src in it, so mount as volume
		volumes = append(volumes, docker.Volume{
			Source:      projectDir,
			Destination: "/src",
		})

		if gpus == "" && cfg.Build.GPU {
			gpus = "all"
		}

	} else {
		// Use existing image
		imageName = args[0]

		// If the image name contains '=', then it's probably a mistake
		if strings.Contains(imageName, "=") {
			return fmt.Errorf("Invalid image name '%s'. Did you forget `-i`?", imageName)
		}

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
		if gpus == "" && conf.Build.GPU {
			gpus = "all"
		}
	}

	console.Info("")
	console.Infof("Starting Docker image %s and running setup()...", imageName)

	predictor := predict.NewPredictor(docker.RunOptions{
		GPUs:    gpus,
		Image:   imageName,
		Volumes: volumes,
		Env:     envFlags,
	})

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
		// Only retry if we're using a GPU but but the user didn't explicitly select a GPU with --gpus
		// If the user specified the wrong GPU, they are explicitly selecting a GPU and they'll want to hear about it
		if gpus == "all" && errors.Is(err, docker.ErrMissingDeviceDriver) {
			console.Info("Missing device driver, re-trying without GPU")

			_ = predictor.Stop()
			predictor = predict.NewPredictor(docker.RunOptions{
				Image:   imageName,
				Volumes: volumes,
				Env:     envFlags,
			})

			if err := predictor.Start(os.Stderr); err != nil {
				return err
			}
		} else {
			return err
		}
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

func isURI(ref *openapi3.Schema) bool {
	return ref != nil && ref.Type.Is("string") && ref.Format == "uri"
}

func predictIndividualInputs(predictor predict.Predictor, inputFlags []string, outputPath string) error {
	console.Info("Running prediction...")
	schema, err := predictor.GetSchema()
	if err != nil {
		return err
	}

	inputs, err := parseInputFlags(inputFlags)
	if err != nil {
		return err
	}

	// Ignore @, to make it behave the same as -i
	outputPath = strings.TrimPrefix(outputPath, "@")

	// Generate output depending on type in schema
	responseSchema := schema.Paths.Value("/predictions").Post.Responses.Value("200").Value.Content["application/json"].Schema.Value
	outputSchema := responseSchema.Properties["output"].Value

	// Multiple outputs!
	if outputSchema.Type.Is("array") && isURI(outputSchema.Items.Value) {
		prediction, err := predictor.Predict(inputs)
		if err != nil {
			return err
		}
		outputs, ok := (*prediction.Output).([]interface{})
		if !ok {
			return fmt.Errorf("Failed to decode output")
		}

		for i, output := range outputs {
			if err := writeDataURLOutput(output.(string), fmt.Sprintf("output.%d", i), true); err != nil {
				return err
			}
		}
		return nil
	}

	// If outputPath != "", then we now know the output path for sure
	if outputPath != "" && checkOutputWritable(outputPath) != nil {
		return err
	}

	prediction, err := predictor.Predict(inputs)
	if err != nil {
		return err
	}

	if isURI(outputSchema) {
		if outputPath == "" {
			return writeDataURLOutput((*prediction.Output).(string), "output", true)
		} else {
			return writeDataURLOutput((*prediction.Output).(string), outputPath, false)
		}
	}

	var out []byte

	if outputSchema.Type.Is("string") {
		// Handle strings separately because if we encode it to JSON it will be surrounded by quotes.
		if prediction.Output == nil {
			console.Warnf("No output generated")
			return nil
		}

		s, ok := (*prediction.Output).(string)
		if !ok {
			return fmt.Errorf("Failed to convert prediction output to string")
		}

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
	return writeOutput(outputPath, out)
}

func checkOutputWritable(outputPath string) error {
	outputPath, err := homedir.Expand(outputPath)
	if err != nil {
		return err
	}

	// Check if the file exists
	_, err = os.Stat(outputPath)
	if err == nil {
		// File exists, check if it's writable
		return unix.Access(outputPath, unix.W_OK)
	} else if os.IsNotExist(err) {
		// File doesn't exist, check if the directory is writable
		dir := filepath.Dir(outputPath)
		return unix.Access(dir, unix.W_OK)
	}

	// Some other error occurred
	return err
}

func writeOutput(outputPath string, output []byte) error {
	outputPath, err := homedir.Expand(outputPath)
	if err != nil {
		return err
	}

	// Write to file
	outFile, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
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

func writeDataURLOutput(outputString string, outputPath string, addExtension bool) error {
	dataurlObj, err := dataurl.DecodeString(outputString)
	if err != nil {
		return fmt.Errorf("Failed to decode dataurl: %w", err)
	}
	out := dataurlObj.Data
	if addExtension {
		extension := mime.ExtensionByType(dataurlObj.ContentType())
		if extension != "" {
			outputPath += extension
		}
	}
	if err := writeOutput(outputPath, out); err != nil {
		return err
	}
	return nil
}

func parseInputFlags(inputs []string) (predict.Inputs, error) {
	keyVals := map[string][]string{}
	for _, input := range inputs {
		var name, value string

		// Default input name is "input"
		if !strings.Contains(input, "=") {
			return nil, fmt.Errorf("Failed to parse input '%s', expected format is 'name=value'", input)
		}

		split := strings.SplitN(input, "=", 2)
		name = split[0]
		value = split[1]

		if strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) {
			value = value[1 : len(value)-1]
		}

		// Append new values to the slice associated with the key
		keyVals[name] = append(keyVals[name], value)
	}

	return predict.NewInputs(keyVals), nil
}
