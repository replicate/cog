package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/TylerBrock/colorjson"
	"github.com/mitchellh/go-homedir"
	"github.com/spf13/cobra"

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

		console.Infof("Pulling image: %s", imageName)
		if err := docker.Pull(imageName); err != nil {
			return fmt.Errorf("Failed to pull %s: %w", imageName, err)
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
		console.Infof("Stopping container...")
		if err := predictor.Stop(); err != nil {
			console.Warnf("Failed to stop container: %s", err)
		}
	}()

	return predictIndividualInputs(predictor, inputFlags, outPath)
}

func predictIndividualInputs(predictor predict.Predictor, inputFlags []string, outputPath string) error {
	console.Info("Running prediction...")
	inputs := parseInputFlags(inputFlags)
	result, err := predictor.Predict(inputs)
	if err != nil {
		return err
	}

	// TODO(andreas): support multiple outputs?
	output := result.Values["output"]

	// Write to stdout
	if outputPath == "" {
		// Is it something we can sensibly write to stdout?
		if output.MimeType == "text/plain" {
			output, err := io.ReadAll(output.Buffer)
			if err != nil {
				return err
			}
			console.Output(string(output))
			return nil
		} else if output.MimeType == "application/json" {
			var obj interface{}
			dec := json.NewDecoder(output.Buffer)
			if err := dec.Decode(&obj); err != nil {
				return err
			}
			f := colorjson.NewFormatter()
			f.Indent = 2
			s, _ := f.Marshal(obj)
			console.Output(string(s))
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
