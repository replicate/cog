package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/mitchellh/go-homedir"
	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/image"
	"github.com/replicate/cog/pkg/predict"
	"github.com/replicate/cog/pkg/registry"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/pkg/util/files"
)

var (
	envFlags             []string
	inputFlags           []string
	outPath              string
	setupTimeout         uint32
	useReplicateAPIToken bool
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
	addSetupTimeoutFlag(cmd)
	addFastFlag(cmd)
	addLocalImage(cmd)
	addConfigFlag(cmd)

	cmd.Flags().StringArrayVarP(&inputFlags, "input", "i", []string{}, "Inputs, in the form name=value. if value is prefixed with @, then it is read from a file on disk. E.g. -i path=@image.jpg")
	cmd.Flags().StringVarP(&outPath, "output", "o", "", "Output path")
	cmd.Flags().StringArrayVarP(&envFlags, "env", "e", []string{}, "Environment variables, in the form name=value")
	cmd.Flags().BoolVar(&useReplicateAPIToken, "use-replicate-token", false, "Pass REPLICATE_API_TOKEN from local environment into the model context")

	return cmd
}

func cmdPredict(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	dockerClient, err := docker.NewClient(ctx)
	if err != nil {
		return err
	}

	imageName := ""
	volumes := []command.Volume{}
	gpus := gpusFlag

	if len(args) == 0 {
		// Build image

		cfg, projectDir, err := config.GetConfig(configFilename)
		if err != nil {
			return err
		}

		if cfg.Build.Fast {
			buildFast = cfg.Build.Fast
		}

		client := registry.NewRegistryClient()
		if buildFast {
			imageName = config.DockerImageName(projectDir)
			if err := image.Build(
				ctx,
				cfg,
				projectDir,
				imageName,
				buildSecrets,
				buildNoCache,
				buildSeparateWeights,
				buildUseCudaBaseImage,
				buildProgressOutput,
				buildSchemaFile,
				buildDockerfileFile,
				DetermineUseCogBaseImage(cmd),
				buildStrip,
				buildPrecompile,
				buildFast,
				nil,
				buildLocalImage,
				dockerClient,
				client); err != nil {
				return err
			}
		} else {
			if imageName, err = image.BuildBase(ctx, dockerClient, cfg, projectDir, buildUseCudaBaseImage, DetermineUseCogBaseImage(cmd), buildProgressOutput, client); err != nil {
				return err
			}

			// Base image doesn't have /src in it, so mount as volume
			volumes = append(volumes, command.Volume{
				Source:      projectDir,
				Destination: "/src",
			})

			if gpus == "" && cfg.Build.GPU {
				gpus = "all"
			}
		}

	} else {
		// Use existing image
		imageName = args[0]

		// If the image name contains '=', then it's probably a mistake
		if strings.Contains(imageName, "=") {
			return fmt.Errorf("Invalid image name '%s'. Did you forget `-i`?", imageName)
		}

		inspectResp, err := dockerClient.Pull(ctx, imageName, false)
		if err != nil {
			return fmt.Errorf("Failed to pull image %q: %w", imageName, err)
		}

		conf, err := image.CogConfigFromManifest(ctx, inspectResp)
		if err != nil {
			return err
		}
		if gpus == "" && conf.Build.GPU {
			gpus = "all"
		}
		if conf.Build.Fast {
			buildFast = conf.Build.Fast
		}
	}

	console.Info("")
	console.Infof("Starting Docker image %s and running setup()...", imageName)

	predictor, err := predict.NewPredictor(ctx, command.RunOptions{
		GPUs:    gpus,
		Image:   imageName,
		Volumes: volumes,
		Env:     envFlags,
	}, false, buildFast, dockerClient)
	if err != nil {
		return err
	}

	go func() {
		captureSignal := make(chan os.Signal, 1)
		signal.Notify(captureSignal, syscall.SIGINT)

		<-captureSignal

		console.Info("Stopping container...")
		if err := predictor.Stop(ctx); err != nil {
			console.Warnf("Failed to stop container: %s", err)
		}
	}()

	timeout := time.Duration(setupTimeout) * time.Second
	if err := predictor.Start(ctx, os.Stderr, timeout); err != nil {
		// Only retry if we're using a GPU but but the user didn't explicitly select a GPU with --gpus
		// If the user specified the wrong GPU, they are explicitly selecting a GPU and they'll want to hear about it
		if gpus == "all" && errors.Is(err, docker.ErrMissingDeviceDriver) {
			console.Info("Missing device driver, re-trying without GPU")

			_ = predictor.Stop(ctx)
			predictor, err = predict.NewPredictor(ctx, command.RunOptions{
				Image:   imageName,
				Volumes: volumes,
				Env:     envFlags,
			}, false, buildFast, dockerClient)
			if err != nil {
				return err
			}

			if err := predictor.Start(ctx, os.Stderr, timeout); err != nil {
				return err
			}
		} else {
			return err
		}
	}

	// FIXME: will not run on signal
	defer func() {
		console.Debugf("Stopping container...")
		// use background context to ensure stop signal is still sent after root context is canceled
		if err := predictor.Stop(context.Background()); err != nil {
			console.Warnf("Failed to stop container: %s", err)
		}
	}()

	return predictIndividualInputs(*predictor, inputFlags, outPath, false)
}

func isURI(ref *openapi3.Schema) bool {
	return ref != nil && ref.Type.Is("string") && ref.Format == "uri"
}

func predictIndividualInputs(predictor predict.Predictor, inputFlags []string, outputPath string, isTrain bool) error {
	if isTrain {
		console.Info("Running training...")
	} else {
		console.Info("Running prediction...")
	}

	schema, err := predictor.GetSchema()
	if err != nil {
		return err
	}

	inputs, err := parseInputFlags(inputFlags, schema)
	if err != nil {
		return err
	}

	// If outputPath != "", then we now know the output path for sure
	if outputPath != "" {
		// Ignore @, to make it behave the same as -i
		outputPath = strings.TrimPrefix(outputPath, "@")

		if err := checkOutputWritable(outputPath); err != nil {
			return fmt.Errorf("Output path is not writable: %w", err)
		}
	}

	// Generate output depending on type in schema
	url := "/predictions"
	if isTrain {
		url = "/trainings"
	}
	responseSchema := schema.Paths.Value(url).Post.Responses.Value("200").Value.Content["application/json"].Schema.Value
	outputSchema := responseSchema.Properties["output"].Value

	context := predict.RequestContext{}

	if useReplicateAPIToken {
		context.ReplicateAPIToken = os.Getenv("REPLICATE_API_TOKEN")
		if context.ReplicateAPIToken == "" {
			return fmt.Errorf("Failed to find REPLICATE_API_TOKEN in the current environment when called with --use-replicate-token")
		}
	}

	prediction, err := predictor.Predict(inputs, context)
	if err != nil {
		return fmt.Errorf("Failed to predict: %w", err)
	}

	if prediction.Output == nil {
		console.Warn("No output generated")
		return nil
	}

	switch {
	case isURI(outputSchema):
		addExtension := false
		if outputPath == "" {
			outputPath = "output"
			addExtension = true
		}

		outputStr, ok := (*prediction.Output).(string)
		if !ok {
			return fmt.Errorf("Failed to convert prediction output to string")
		}

		err := files.WriteDataURLOutput(outputStr, outputPath, addExtension)
		if err != nil {
			return fmt.Errorf("Failed to write output: %w", err)
		}

		return nil
	case outputSchema.Type.Is("array") && isURI(outputSchema.Items.Value):
		outputs, ok := (*prediction.Output).([]interface{})
		if !ok {
			return fmt.Errorf("Failed to decode output")
		}

		for i, output := range outputs {
			outputPath := fmt.Sprintf("output.%d", i)
			addExtension := true

			outputStr, ok := output.(string)
			if !ok {
				return fmt.Errorf("Failed to convert prediction output to string")
			}

			err := files.WriteDataURLOutput(outputStr, outputPath, addExtension)
			if err != nil {
				return fmt.Errorf("Failed to write output: %w", err)
			}
		}

		return nil
	case outputSchema.Type.Is("string"):
		s, ok := (*prediction.Output).(string)
		if !ok {
			return fmt.Errorf("Failed to convert prediction output to string")
		}

		if outputPath == "" {
			console.Output(s)
		} else {
			err := files.WriteOutput(outputPath, []byte(s))
			if err != nil {
				return fmt.Errorf("Failed to write output: %w", err)
			}
		}

		return nil
	default:
		// Treat everything else as JSON -- ints, floats, bools will all convert correctly.
		rawJSON, err := json.Marshal(prediction.Output)
		if err != nil {
			return fmt.Errorf("Failed to encode prediction output as JSON: %w", err)
		}
		var indentedJSON bytes.Buffer
		if err := json.Indent(&indentedJSON, rawJSON, "", "  "); err != nil {
			return err
		}

		if outputPath == "" {
			console.Output(indentedJSON.String())
		} else {
			err := files.WriteOutput(outputPath, indentedJSON.Bytes())
			if err != nil {
				return fmt.Errorf("Failed to write output: %w", err)
			}
		}

		return nil
	}
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

func parseInputFlags(inputs []string, schema *openapi3.T) (predict.Inputs, error) {
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

	return predict.NewInputs(keyVals, schema)
}

func addSetupTimeoutFlag(cmd *cobra.Command) {
	cmd.Flags().Uint32Var(&setupTimeout, "setup-timeout", 5*60, "The timeout for a container to setup (in seconds).")
}
