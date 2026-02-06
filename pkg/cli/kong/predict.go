package kong

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
	"golang.org/x/sys/unix"

	"github.com/replicate/cog/pkg/docker"
	"github.com/replicate/cog/pkg/docker/command"
	"github.com/replicate/cog/pkg/model"
	r8_path "github.com/replicate/cog/pkg/path"
	"github.com/replicate/cog/pkg/predict"
	"github.com/replicate/cog/pkg/registry"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/pkg/util/files"
	"github.com/replicate/cog/pkg/util/mime"
)

const stdinPath = "-"

// PredictCmd implements `cog predict [image]`.
type PredictCmd struct {
	Image string `arg:"" optional:"" help:"Docker image to run prediction on"`

	Input        []string `help:"Inputs, in the form name=value" short:"i" name:"input"`
	Output       string   `help:"Output path" short:"o"`
	Env          []string `help:"Environment variables, in the form name=value" short:"e"`
	UseReplToken bool     `help:"Pass REPLICATE_API_TOKEN into the model context" name:"use-replicate-token"`
	JSON         string   `help:"Pass inputs as JSON object, file (@path), or stdin (@-)" name:"json"`
	SetupTimeout uint32   `help:"Container setup timeout in seconds" name:"setup-timeout" default:"300"`

	GPUFlags   `embed:""`
	BuildFlags `embed:""`
}

func (c *PredictCmd) Run(g *Globals) error {
	ctx := contextFromGlobals(g)

	dockerClient, err := docker.NewClient(ctx)
	if err != nil {
		return err
	}

	imageName := ""
	volumes := []command.Volume{}
	gpus := c.GPUs

	resolver := model.NewResolver(dockerClient, registry.NewRegistryClient())

	if c.Image == "" {
		src, err := model.NewSource(c.ConfigFile)
		if err != nil {
			return err
		}
		m, err := resolver.BuildBase(ctx, src, c.BuildFlags.BuildBaseOptions())
		if err != nil {
			return err
		}
		imageName = m.ImageRef()
		volumes = append(volumes, command.Volume{Source: src.ProjectDir, Destination: "/src"})
		if gpus == "" && m.HasGPU() {
			gpus = "all"
		}
	} else {
		imageName = c.Image
		if strings.Contains(imageName, "=") {
			return fmt.Errorf("Invalid image name '%s'. Did you forget `-i`?", imageName)
		}
		ref, err := model.ParseRef(imageName)
		if err != nil {
			return err
		}
		m, err := resolver.Pull(ctx, ref)
		if err != nil {
			return err
		}
		if gpus == "" && m.HasGPU() {
			gpus = "all"
		}
	}

	console.Info("")
	console.Infof("Starting Docker image %s and running setup()...", imageName)

	env := propagateRustLog(c.Env)

	predictor, err := predict.NewPredictor(ctx, command.RunOptions{
		GPUs:    gpus,
		Image:   imageName,
		Volumes: volumes,
		Env:     env,
	}, false, dockerClient)
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

	timeout := time.Duration(c.SetupTimeout) * time.Second
	if err := predictor.Start(ctx, os.Stderr, timeout); err != nil {
		if gpus == "all" && c.GPUFlags.IsAutoDetected() && errors.Is(err, docker.ErrMissingDeviceDriver) {
			console.Info("Missing device driver, re-trying without GPU")
			_ = predictor.Stop(ctx)
			predictor, err = predict.NewPredictor(ctx, command.RunOptions{
				Image:   imageName,
				Volumes: volumes,
				Env:     env,
			}, false, dockerClient)
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

	defer func() {
		console.Debugf("Stopping container...")
		if err := predictor.Stop(context.Background()); err != nil {
			console.Warnf("Failed to stop container: %s", err)
		}
	}()

	if c.JSON != "" {
		if len(c.Input) > 0 {
			return fmt.Errorf("Must use one of --json or --input to provide model inputs")
		}
		return predictJSONInputs(*predictor, c.JSON, c.Output, c.UseReplToken, false)
	}
	return predictIndividualInputs(*predictor, c.Input, c.Output, c.UseReplToken, false)
}

// --- Shared prediction helpers (used by both predict and train) ---

func predictJSONInputs(predictor predict.Predictor, jsonInput string, outputPath string, useReplToken bool, isTrain bool) error {
	jsonInputs, err := parseJSONInput(jsonInput)
	if err != nil {
		return err
	}

	transformedInputs, err := transformPathsToBase64URLs(jsonInputs)
	if err != nil {
		return err
	}

	inputs := make(predict.Inputs)
	for key, value := range transformedInputs {
		if strValue, ok := value.(string); ok {
			inputs[key] = predict.Input{String: &strValue}
		} else {
			jsonBytes, err := json.Marshal(value)
			if err != nil {
				return fmt.Errorf("Failed to marshal input %q to JSON: %w", key, err)
			}
			jsonRaw := json.RawMessage(jsonBytes)
			inputs[key] = predict.Input{Json: &jsonRaw}
		}
	}

	return runPrediction(predictor, inputs, outputPath, useReplToken, isTrain, true)
}

func predictIndividualInputs(predictor predict.Predictor, inputFlags []string, outputPath string, useReplToken bool, isTrain bool) error {
	schema, err := predictor.GetSchema()
	if err != nil {
		return err
	}

	inputs, err := parseInputFlags(inputFlags, schema)
	if err != nil {
		return err
	}

	return runPrediction(predictor, inputs, outputPath, useReplToken, isTrain, false)
}

func runPrediction(predictor predict.Predictor, inputs predict.Inputs, outputPath string, useReplToken bool, isTrain bool, needsJSON bool) error {
	if isTrain {
		console.Info("Running training...")
	} else {
		console.Info("Running prediction...")
	}

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

	reqContext := predict.RequestContext{}
	if useReplToken {
		reqContext.ReplicateAPIToken = os.Getenv("REPLICATE_API_TOKEN")
		if reqContext.ReplicateAPIToken == "" {
			return fmt.Errorf("Failed to find REPLICATE_API_TOKEN in the current environment when called with --use-replicate-token")
		}
	}

	prediction, err := predictor.Predict(inputs, reqContext)
	if err != nil {
		return fmt.Errorf("Failed to predict: %w", err)
	}

	schema, err := predictor.GetSchema()
	if err != nil {
		return err
	}

	var outputSchema *openapi3.Schema
	if pathItem := schema.Paths.Value(url); pathItem != nil {
		if pathItem.Post != nil {
			if resp := pathItem.Post.Responses.Value("200"); resp != nil && resp.Value != nil {
				if content, ok := resp.Value.Content["application/json"]; ok && content.Schema != nil {
					if content.Schema.Value != nil {
						if outputProp, ok := content.Schema.Value.Properties["output"]; ok && outputProp != nil {
							outputSchema = outputProp.Value
						}
					}
				}
			}
		}
	}
	if outputSchema == nil {
		return fmt.Errorf("invalid OpenAPI schema: missing output definition for %s", url)
	}

	fileOutputPath := outputPath
	if needsJSON {
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
			p, err := files.WriteFile(indentedJSON.Bytes(), outputPath)
			if err != nil {
				return fmt.Errorf("Failed to write output: %w", err)
			}
			console.Infof("Written output to: %s", p)
		} else {
			console.Output(indentedJSON.String())
		}

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

	switch {
	case isURI(outputSchema):
		return nil
	case outputSchema.Type.Is("array") && isURI(outputSchema.Items.Value):
		return nil
	case outputSchema.Type.Is("string"):
		s, ok := (*prediction.Output).(string)
		if !ok {
			return fmt.Errorf("Failed to convert prediction output to string")
		}
		if writeOutputToDisk {
			p, err := files.WriteFile([]byte(s), outputPath)
			if err != nil {
				return fmt.Errorf("Failed to write output: %w", err)
			}
			console.Infof("Written output to: %s", p)
		} else {
			console.Output(s)
		}
		return nil
	default:
		output, err := prettyJSONMarshal(prediction.Output)
		if err != nil {
			return err
		}
		if writeOutputToDisk {
			p, err := files.WriteFile(output, outputPath)
			if err != nil {
				return fmt.Errorf("Failed to write output: %w", err)
			}
			console.Infof("Written output to: %s", p)
		} else {
			console.Output(string(output))
		}
		return nil
	}
}

// --- Input/output helpers ---

func isURI(ref *openapi3.Schema) bool {
	return ref != nil && ref.Type.Is("string") && ref.Format == "uri"
}

func readStdin() (string, error) {
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
		source := jsonInput[1:]
		if source == stdinPath {
			s, err := readStdin()
			if err != nil {
				return nil, err
			}
			jsonStr = s
		} else {
			data, err := os.ReadFile(source)
			if err != nil {
				return nil, fmt.Errorf("Failed to read JSON from file %q: %w", source, err)
			}
			jsonStr = string(data)
		}
	case jsonInput == stdinPath:
		s, err := readStdin()
		if err != nil {
			return nil, err
		}
		jsonStr = s
	default:
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
			filePath := strValue[1:]
			data, err := os.ReadFile(filePath)
			if err != nil {
				return nil, fmt.Errorf("Failed to read file %q: %w", filePath, err)
			}
			mimeType := mime.TypeByExtension(filepath.Ext(filePath))
			if mimeType == "" {
				mimeType = "application/octet-stream"
			}
			base64Data := base64.StdEncoding.EncodeToString(data)
			result[key] = fmt.Sprintf("data:%s;base64,%s", mimeType, base64Data)
		} else {
			result[key] = value
		}
	}
	return result, nil
}

func parseInputFlags(inputs []string, schema *openapi3.T) (predict.Inputs, error) {
	keyVals := map[string][]string{}
	for _, input := range inputs {
		if !strings.Contains(input, "=") {
			return nil, fmt.Errorf("Failed to parse input '%s', expected format is 'name=value'", input)
		}
		split := strings.SplitN(input, "=", 2)
		name := split[0]
		value := split[1]
		if strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) {
			value = value[1 : len(value)-1]
		}
		keyVals[name] = append(keyVals[name], value)
	}
	return predict.NewInputs(keyVals, schema)
}

func ensureOutputWriteable(outputPath string, fallbackPath string) (string, error) {
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
	if os.IsNotExist(err) {
		if err = unix.Access(path.Dir(outputPath), unix.W_OK); err != nil {
			return "", fmt.Errorf("Output directory is not writable: %s", path.Dir(outputPath))
		}
		return outputPath, nil
	} else if err != nil {
		return "", fmt.Errorf("Unexpected error checking output path: %w", err)
	}

	if stat.IsDir() {
		if usingFallback {
			return "", fmt.Errorf("Default output name %q conflicts with directory, provide --output", outputPath)
		}
		if err := unix.Access(outputPath, unix.W_OK); err != nil {
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
	switch {
	case isURI(schema):
		outputStr, ok := output.(string)
		if !ok {
			return nil, fmt.Errorf("Failed to convert prediction output to string: %v", output)
		}
		p, err := files.WriteDataURLToFile(outputStr, destination)
		if err != nil {
			return nil, fmt.Errorf("Failed to write output: %w", err)
		}
		console.Infof("Written output to: %s", p)
		return any(p), nil
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
