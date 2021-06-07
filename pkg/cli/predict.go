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
	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/logger"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/serving"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/pkg/util/mime"
	"github.com/replicate/cog/pkg/util/slices"
	"github.com/replicate/cog/pkg/util/terminal"
)

var (
	inputs      []string
	outPath     string
	predictArch string
)

func newPredictCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "predict [version id]",
		Short: "Run a prediction on a version",
		Long: `Run a prediction on a version.
		
If 'version id' is passed, it will run the prediction on that version of the 
model. Otherwise, it will build the model in the current directory and run
the prediction on that.`,
		RunE:       cmdPredict,
		Args:       cobra.MaximumNArgs(1),
		SuggestFor: []string{"infer"},
	}
	addModelFlag(cmd)
	cmd.Flags().StringArrayVarP(&inputs, "input", "i", []string{}, "Inputs, in the form name=value. if value is prefixed with @, then it is read from a file on disk. E.g. -i path=@image.jpg")
	cmd.Flags().StringVarP(&outPath, "output", "o", "", "Output path")
	cmd.Flags().StringVarP(&predictArch, "arch", "a", "cpu", "Architecture to run prediction on (cpu/gpu)")

	return cmd
}

func cmdPredict(cmd *cobra.Command, args []string) error {
	if !slices.ContainsString([]string{"cpu", "gpu"}, predictArch) {
		return fmt.Errorf("--arch must be either 'cpu' or 'gpu'")
	}

	ui := terminal.ConsoleUI(context.Background())
	defer ui.Close()

	useGPU := predictArch == "gpu"
	dockerImageName := ""

	if len(args) == 0 {
		// Local

		config, projectDir, err := config.GetConfig(projectDirFlag)
		if err != nil {
			return err
		}
		ui.Output("Building Docker image from environment in cog.yaml...")
		// FIXME: refactor to share with predict
		logWriter := logger.NewTerminalLogger(ui)
		generator := docker.NewDockerfileGenerator(config, predictArch, projectDir)
		defer func() {
			if err := generator.Cleanup(); err != nil {
				ui.Output(fmt.Sprintf("Error cleaning up Dockerfile generator: %s", err))
			}
		}()
		dockerfileContents, err := generator.Generate()
		if err != nil {
			return fmt.Errorf("Failed to generate Dockerfile for %s: %w", predictArch, err)
		}
		dockerImageBuilder := docker.NewLocalImageBuilder("")
		dockerImageName, err = dockerImageBuilder.Build(context.Background(), projectDir, dockerfileContents, "", useGPU, logWriter)
		if err != nil {
			return fmt.Errorf("Failed to build Docker image: %w", err)
		}

		logWriter.Done()

	} else {
		// Remote

		id := args[0]
		mod, err := getModel()
		if err != nil {
			return err
		}
		client := client.NewClient()
		st := ui.Status()
		defer st.Close()
		st.Update("Loading version " + id)
		version, err := client.GetVersion(mod, id)
		st.Step(terminal.StatusOK, "Loaded version "+id)
		if err != nil {
			return err
		}
		image := model.ImageForArch(version.Images, predictArch)
		// TODO(bfirsh): differentiate between failed builds and in-progress builds, and probably block here if there is an in-progress build
		if image == nil {
			return fmt.Errorf("No %s image has been built for %s:%s", predictArch, mod.String(), id)
		}
		dockerImageName = image.URI
	}

	st := ui.Status()
	defer st.Close()
	st.Update(fmt.Sprintf("Starting Docker image %s and running setup()...", dockerImageName))
	servingPlatform, err := serving.NewLocalDockerPlatform()
	if err != nil {
		st.Step(terminal.StatusError, "Failed to start model: "+err.Error())
		return err
	}
	logWriter := logger.NewConsoleLogger()
	deployment, err := servingPlatform.Deploy(context.Background(), dockerImageName, useGPU, logWriter)
	if err != nil {
		st.Step(terminal.StatusError, "Failed to start model: "+err.Error())
		return err
	}
	defer func() {
		if err := deployment.Undeploy(); err != nil {
			console.Warnf("Failed to kill Docker container: %s", err)
		}
	}()
	st.Step(terminal.StatusOK, fmt.Sprintf("Model running in Docker image %s", dockerImageName))

	return predictIndividualInputs(ui, deployment, inputs, outPath, logWriter)
}

func predictIndividualInputs(ui terminal.UI, deployment serving.Deployment, inputs []string, outputPath string, logWriter logger.Logger) error {
	st := ui.Status()
	defer st.Close()
	st.Update("Running prediction...")
	example := parsePredictInputs(inputs)
	result, err := deployment.RunPrediction(context.Background(), example, logWriter)
	if err != nil {
		st.Step(terminal.StatusError, "Failed to run prediction: "+err.Error())
		return err
	}
	st.Close()

	// TODO(andreas): support multiple outputs?
	output := result.Values["output"]

	ui.Output("")

	// Write to stdout
	if outputPath == "" {
		// Is it something we can sensibly write to stdout?
		if output.MimeType == "text/plain" {
			output, err := io.ReadAll(output.Buffer)
			if err != nil {
				return err
			}
			ui.Output(string(output))
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
			ui.Output(string(s))
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

	ui.Output("Written output to " + outputPath)
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
