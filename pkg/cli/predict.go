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

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/dockerfile"
	"github.com/replicate/cog/pkg/logger"
	"github.com/replicate/cog/pkg/serving"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/pkg/util/mime"
	"github.com/replicate/cog/pkg/util/slices"
)

var (
	inputs      []string
	outPath     string
	predictArch string
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
	cmd.Flags().StringArrayVarP(&inputs, "input", "i", []string{}, "Inputs, in the form name=value. if value is prefixed with @, then it is read from a file on disk. E.g. -i path=@image.jpg")
	cmd.Flags().StringVarP(&outPath, "output", "o", "", "Output path")
	cmd.Flags().StringVarP(&predictArch, "arch", "a", "cpu", "Architecture to run prediction on (cpu/gpu)")

	return cmd
}

func cmdPredict(cmd *cobra.Command, args []string) error {
	if !slices.ContainsString([]string{"cpu", "gpu"}, predictArch) {
		return fmt.Errorf("--arch must be either 'cpu' or 'gpu'")
	}

	useGPU := predictArch == "gpu"
	image := ""

	if len(args) == 0 {
		// Build image

		cfg, projectDir, err := config.GetConfig(projectDirFlag)
		if err != nil {
			return err
		}

		// TODO: better image management so we don't eat up disk space
		image = config.DockerImageName(projectDir)

		console.Info("Building Docker image from environment in cog.yaml...")
		// FIXME: refactor to share with run
		generator := dockerfile.NewGenerator(cfg, predictArch, projectDir)
		defer func() {
			if err := generator.Cleanup(); err != nil {
				console.Warnf("Error cleaning up Dockerfile generator: %s", err)
			}
		}()
		dockerfileContents, err := generator.Generate()
		if err != nil {
			return fmt.Errorf("Failed to generate Dockerfile for %s: %w", predictArch, err)
		}
		if err := docker.Build(projectDir, dockerfileContents, image); err != nil {
			return fmt.Errorf("Failed to build Docker image: %w", err)
		}
	} else {
		// Use existing image
		image = args[0]
	}

	console.Infof("Starting Docker image %s and running setup()...", image)
	servingPlatform, err := serving.NewLocalDockerPlatform()
	if err != nil {
		return err
	}
	deployLogWriter := logger.NewConsoleLogger()
	deployment, err := servingPlatform.Deploy(context.Background(), image, useGPU, deployLogWriter)
	if err != nil {
		return err
	}
	defer func() {
		if err := deployment.Undeploy(); err != nil {
			console.Warnf("Failed to kill Docker container: %s", err)
		}
	}()

	return predictIndividualInputs(deployment, inputs, outPath, deployLogWriter)
}

func predictIndividualInputs(deployment serving.Deployment, inputs []string, outputPath string, logWriter logger.Logger) error {
	console.Info("Running prediction...")
	example := parsePredictInputs(inputs)
	result, err := deployment.RunPrediction(context.Background(), example, logWriter)
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
			var obj map[string]interface{}
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

func parsePredictInputs(inputs []string) *serving.Example {
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
