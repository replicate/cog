package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path"
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
	r8_path "github.com/replicate/cog/pkg/path"
	"github.com/replicate/cog/pkg/predict"
	"github.com/replicate/cog/pkg/registry"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/pkg/util/files"
	"github.com/replicate/cog/pkg/util/mime"
)

const StdinPath = "-"

var (
	envFlags             []string
	inputFlags           []string
	outPath              string
	setupTimeout         uint32
	useReplicateAPIToken bool
	inputJSON            string
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
	addPipelineImage(cmd)

	cmd.Flags().StringArrayVarP(&inputFlags, "input", "i", []string{}, "Inputs, in the form name=value. if value is prefixed with @, then it is read from a file on disk. E.g. -i path=@image.jpg")
	cmd.Flags().StringVarP(&outPath, "output", "o", "", "Output path")
	cmd.Flags().StringArrayVarP(&envFlags, "env", "e", []string{}, "Environment variables, in the form name=value")
	cmd.Flags().BoolVar(&useReplicateAPIToken, "use-replicate-token", false, "Pass REPLICATE_API_TOKEN from local environment into the model context")
	cmd.Flags().StringVar(&inputJSON, "json", "", "Pass inputs as JSON object, read from file (@inputs.json) or via stdin (@-)")

	return cmd
}

func readStdin() (string, error) {
	// Read from stdin
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", fmt.Errorf("Failed to read JSON from stdin: %w", err)
	}
	return string(data), nil
}

func parseJSONInput(jsonInput string) (map[string]any, error) {
	var jsonStr string

	switch {
	case strings.HasPrefix(jsonInput, "@"):
		// Read from file or stdin
		source := jsonInput[1:]

		if source == StdinPath {
			jsonStdinStr, err := readStdin()
			if err != nil {
				return nil, err
			}
			jsonStr = jsonStdinStr
		} else {
			// Read from file
			data, err := os.ReadFile(source)
			if err != nil {
				return nil, fmt.Errorf("Failed to read JSON from file %q: %w", source, err)
			}
			jsonStr = string(data)
		}
	case jsonInput == StdinPath:
		jsonStdinStr, err := readStdin()
		if err != nil {
			return nil, err
		}
		jsonStr = jsonStdinStr
	default:
		// Direct JSON string
		jsonStr = jsonInput
	}

	var inputs map[string]any
	if err := json.Unmarshal([]byte(jsonStr), &inputs); err != nil {
		return nil, fmt.Errorf("Failed to parse JSON: %w", err)
	}

	return inputs, nil
}

func transformPathsToBase64URLs(inputs map[string]any) (map[string]any, error) {
	result := make(map[string]any)

	for key, value := range inputs {
		if strValue, ok := value.(string); ok && strings.HasPrefix(strValue, "@") {
			// This is a file path, convert to base64 data URL
			filePath := strValue[1:]

			// Read file
			data, err := os.ReadFile(filePath)
			if err != nil {
				return nil, fmt.Errorf("Failed to read file %q: %w", filePath, err)
			}

			// Get MIME type
			mimeType := mime.TypeByExtension(filepath.Ext(filePath))
			if mimeType == "" {
				mimeType = "application/octet-stream"
			}

			// Create base64 data URL
			base64Data := base64.StdEncoding.EncodeToString(data)
			dataURL := fmt.Sprintf("data:%s;base64,%s", mimeType, base64Data)

			result[key] = dataURL
		} else {
			result[key] = value
		}
	}

	return result, nil
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
		if buildFast || pipelinesImage {
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
				client,
				pipelinesImage); err != nil {
				return err
			}
		} else {
			if imageName, err = image.BuildBase(ctx, dockerClient, cfg, projectDir, buildUseCudaBaseImage, DetermineUseCogBaseImage(cmd), buildProgressOutput, client, true); err != nil {
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

	if inputJSON != "" {
		if len(inputFlags) > 0 {
			return fmt.Errorf("Must use one of --json or --input to provide model inputs")
		}

		return predictJSONInputs(*predictor, inputJSON, outPath, false)
	}
	return predictIndividualInputs(*predictor, inputFlags, outPath, false)
}

func isURI(ref *openapi3.Schema) bool {
	return ref != nil && ref.Type.Is("string") && ref.Format == "uri"
}

func predictJSONInputs(predictor predict.Predictor, jsonInput string, outputPath string, isTrain bool) error {
	jsonInputs, err := parseJSONInput(jsonInput)
	if err != nil {
		return err
	}

	transformedInputs, err := transformPathsToBase64URLs(jsonInputs)
	if err != nil {
		return err
	}

	// Convert to predict.Inputs format
	inputs := make(predict.Inputs)
	for key, value := range transformedInputs {
		if strValue, ok := value.(string); ok {
			inputs[key] = predict.Input{String: &strValue}
		} else {
			// For non-string values, marshal to JSON
			jsonBytes, err := json.Marshal(value)
			if err != nil {
				return fmt.Errorf("Failed to marshal input %q to JSON: %w", key, err)
			}
			jsonRaw := json.RawMessage(jsonBytes)
			inputs[key] = predict.Input{Json: &jsonRaw}
		}
	}

	return runPrediction(predictor, inputs, outputPath, isTrain, true)
}

func predictIndividualInputs(predictor predict.Predictor, inputFlags []string, outputPath string, isTrain bool) error {
	schema, err := predictor.GetSchema()
	if err != nil {
		return err
	}

	inputs, err := parseInputFlags(inputFlags, schema)
	if err != nil {
		return err
	}

	return runPrediction(predictor, inputs, outputPath, isTrain, false)
}

func runPrediction(predictor predict.Predictor, inputs predict.Inputs, outputPath string, isTrain bool, needsJSON bool) error {
	if isTrain {
		console.Info("Running training...")
	} else {
		console.Info("Running prediction...")
	}

	// Generate output depending on type in schema
	url := "/predictions"
	if isTrain {
		url = "/trainings"
	}

	writeOutputToDisk := outputPath != ""
	fallbackPath := "output"
	if needsJSON {
		fallbackPath = "output.json"
	}

	outputPath, err := ensureOutputWriteable(strings.TrimPrefix(outputPath, "@"), fallbackPath)
	if err != nil {
		return fmt.Errorf("Output path is not writable: %w", err)
	}

	if needsJSON && !strings.HasSuffix(outputPath, ".json") {
		console.Warnf("--output value does not have a .json suffix: %s", path.Base(outputPath))
	}

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

	schema, err := predictor.GetSchema()
	if err != nil {
		return err
	}
	outputSchema := schema.Paths.Value(url).Post.Responses.Value("200").Value.Content["application/json"].Schema.Value.Properties["output"].Value

	fileOutputPath := outputPath
	if needsJSON {
		// Strip the suffix when in JSON mode.
		fileOutputPath = r8_path.TrimExt(fileOutputPath)
	}

	if prediction.Status == "succeeded" && prediction.Output != nil {
		transformed, err := processFileOutputs(*prediction.Output, outputSchema, fileOutputPath)
		if err != nil {
			return err
		}
		prediction.Output = &transformed
	}

	if needsJSON {
		rawJSON, err := json.Marshal(prediction)
		if err != nil {
			return fmt.Errorf("Failed to encode prediction output as JSON: %w", err)
		}
		var indentedJSON bytes.Buffer
		if err := json.Indent(&indentedJSON, rawJSON, "", "  "); err != nil {
			return err
		}

		if writeOutputToDisk {
			path, err := files.WriteFile(indentedJSON.Bytes(), outputPath)
			if err != nil {
				return fmt.Errorf("Failed to write output: %w", err)
			}
			console.Infof("Written output to: %s", path)
		} else {
			console.Output(indentedJSON.String())
		}

		// Exit with non-zero code if the prediction has failed.
		if prediction.Status != "succeeded" {
			os.Exit(1)
		}

		return nil
	}

	if prediction.Status != "succeeded" {
		return fmt.Errorf("Prediction failed with status %q: %s", prediction.Status, prediction.Error)
	}

	if prediction.Output == nil {
		console.Warn("No output generated")
		return nil
	}

	// Handle default presentation of output types.
	// 1. For Path and list[Path] do nothing. We already print info for each file write.
	// 2. For everything else we want to print the raw value.
	switch {
	case isURI(outputSchema):
		return nil
	case outputSchema.Type.Is("array") && isURI(outputSchema.Items.Value):
		return nil
	case outputSchema.Type.Is("string"):
		// Output the raw string.
		s, ok := (*prediction.Output).(string)
		if !ok {
			return fmt.Errorf("Failed to convert prediction output to string")
		}

		if writeOutputToDisk {
			path, err := files.WriteFile([]byte(s), outputPath)
			if err != nil {
				return fmt.Errorf("Failed to write output: %w", err)
			}
			console.Infof("Written output to: %s", path)
		} else {
			console.Output(s)
		}

		return nil
	default:
		// Treat everything else as JSON -- ints, floats, bools will all be presented
		// as raw values. Lists and objects will be pretty printed JSON.
		output, err := prettyJSONMarshal(prediction.Output)
		if err != nil {
			return err
		}

		// No special handling for needsJSON here.
		if writeOutputToDisk {
			path, err := files.WriteFile(output, outputPath)
			if err != nil {
				return fmt.Errorf("Failed to write output: %w", err)
			}
			console.Infof("Written output to: %s", path)
		} else {
			console.Output(string(output))
		}

		return nil
	}
}

// Ensures the path (or fallback) provided is writable. Returns path, error
func ensureOutputWriteable(outputPath string, fallbackPath string) (string, error) {
	// If no outputPath is provided use fallback path and track.
	usingFallback := false
	if outputPath == "" {
		outputPath = fallbackPath
		usingFallback = true
	}

	outputPath, err := homedir.Expand(outputPath)
	if err != nil {
		return "", err
	}

	stat, err := os.Stat(outputPath)

	// If the file doesn't exist, use the parent directory with given filename.
	if os.IsNotExist(err) {
		if err = unix.Access(path.Dir(outputPath), unix.W_OK); err != nil {
			return "", fmt.Errorf("Output directory is not writable: %s", path.Dir(outputPath))
		}
		return outputPath, nil
	} else if err != nil {
		return "", fmt.Errorf("Unexpected error checking output path: %w", err)
	}

	// If a directory was provided, use that with the fallback filename
	if stat.IsDir() {
		// If the fallback path already exists as a directory error.
		if usingFallback {
			return "", fmt.Errorf("Default output name %q conflicts with directory, provide --output", outputPath)
		}
		err := unix.Access(outputPath, unix.W_OK)
		if err != nil {
			return "", err
		}
		return path.Join(outputPath, path.Base(fallbackPath)), nil
	}

	if err = unix.Access(outputPath, unix.W_OK); err != nil {
		return "", err
	}

	return outputPath, nil
}

func prettyJSONMarshal(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return []byte(""), fmt.Errorf("Failed to encode JSON: %w", err)
	}
	var formatted bytes.Buffer
	if err := json.Indent(&formatted, raw, "", "  "); err != nil {
		return []byte(""), err
	}
	return formatted.Bytes(), nil
}

func processFileOutputs(output any, schema *openapi3.Schema, destination string) (any, error) {
	// TODO: This doesn't currently support arbitrary objects.
	switch {
	case isURI(schema):
		outputStr, ok := output.(string)
		if !ok {
			return nil, fmt.Errorf("Failed to convert prediction output to string: %v", output)
		}

		path, err := files.WriteDataURLToFile(outputStr, destination)
		if err != nil {
			return nil, fmt.Errorf("Failed to write output: %w", err)
		}
		console.Infof("Written output to: %s", path)

		return any(path), nil
	case schema.Type.Is("array") && isURI(schema.Items.Value):
		outputs, ok := (output).([]any)
		if !ok {
			return nil, fmt.Errorf("Failed to decode output: %v", output)
		}

		clone := []any{}
		for i, output := range outputs {
			itemDestination := fmt.Sprintf("%s.%d%s", r8_path.TrimExt(destination), i, path.Ext(destination))
			item, err := processFileOutputs(output, schema.Items.Value, itemDestination)
			if err != nil {
				return nil, fmt.Errorf("Failed to write output %d: %w", i, err)
			}

			clone = append(clone, item)
		}

		return clone, nil
	}

	return output, nil
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
