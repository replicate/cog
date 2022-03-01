package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"

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
	})
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
	inputs := parseInputFlags(inputFlags)
	prediction, err := predictor.Predict(inputs)
	if err != nil {
		return err
	}
	schema, err := predictor.GetSchema()
	if err != nil {
		return err
	}

	// Generate output depending on type in schema
	var out []byte
	outputSchema := schema.Components.Schemas["Response"].Value.Properties["output"].Value
	if outputSchema.Type == "string" && outputSchema.Format == "uri" {
		dataurlObj, err := dataurl.DecodeString((*prediction.Output).(string))
		if err != nil {
			return fmt.Errorf("Failed to decode dataurl: %w", err)
		}
		out = dataurlObj.Data
		outputPath = "output"
		extension := mime.ExtensionByType(dataurlObj.ContentType())
		if extension != "" {
			outputPath += extension
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

	outputPath, err = homedir.Expand(outputPath)
	if err != nil {
		return err
	}

	// Write to file
	outFile, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE, 0o755)
	if err != nil {
		return err
	}

	if _, err := outFile.Write(out); err != nil {
		return err
	}
	if err := outFile.Close(); err != nil {
		return err
	}

	console.Infof("Written output to %s", outputPath)
	return nil
}

func parseInputFlags(inputs []string) predict.Inputs {
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
	return predict.NewInputs(keyVals)
}
