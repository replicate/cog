package serving

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/montanaflynn/stats"
	"github.com/replicate/cog/pkg/logger"
	"github.com/replicate/cog/pkg/model"
)

// TODO(andreas): put this somewhere else since it's used by server?
const ExampleOutputDir = "cog-example-output"

type TestResult struct {
	RunArgs           map[string]*model.RunArgument
	Examples          []*model.Example
	NewExampleOutputs map[string][]byte // map of paths (e.g. "cog-example-output/output.01.png") to contents
	Stats             *model.Stats
}

// TestModel runs the example inputs and checks the example
// outputs. If examples inputs are defined but example outputs aren't,
// defined, the resulting outputs are written to exampleOutputDir and
// the examples object is updated to point to those outputs.
func TestModel(ctx context.Context, servingPlatform Platform, imageTag string, examples []*model.Example, dir string, useGPU bool, logWriter logger.Logger) (*TestResult, error) {
	logWriter.WriteStatus("Testing model")

	// copy examples to avoid race conditions when building in parallel
	examples = copyExamples(examples)
	newExampleOutputs := make(map[string][]byte)
	modelStats := new(model.Stats)

	bootStart := time.Now()
	deployment, err := servingPlatform.Deploy(ctx, imageTag, useGPU, logWriter)
	defer func() {
		if deployment != nil {
			deployment.Undeploy()
		}
	}()
	if err != nil {
		return nil, err
	}

	modelStats.BootTime = time.Since(bootStart).Seconds()

	help, err := deployment.Help(ctx, logWriter)
	if err != nil {
		return nil, err
	}

	setupTimes := []float64{}
	runTimes := []float64{}
	memoryUsages := []float64{}
	cpuUsages := []float64{}
	for index, example := range examples {
		if err := validateServingExampleInput(help.Arguments, example.Input); err != nil {
			return nil, fmt.Errorf("Example input doesn't match run arguments: %w", err)
		}
		expectedOutput, outputIsFile, err := outputBytesFromExample(example.Output, dir)
		if err != nil {
			return nil, err
		}

		input := NewExampleWithBaseDir(example.Input, dir)

		result, err := deployment.RunInference(ctx, input, logWriter)
		if err != nil {
			return nil, err
		}
		logWriter.Debugf("Memory usage (bytes): %d", result.UsedMemoryBytes)
		logWriter.Debugf("CPU usage (seconds):  %.1f", result.UsedCPUSecs)

		output := result.Values["output"]
		outputBytes, err := io.ReadAll(output.Buffer)
		if err != nil {
			return nil, fmt.Errorf("Failed to read output: %w", err)
		}
		logWriter.Infof("Inference result length: %d, mime type: %s", len(outputBytes), output.MimeType)
		if expectedOutput == nil {
			updateExampleOutput(example, newExampleOutputs, outputBytes, output.MimeType, index)
		} else {
			if err := verifyCorrectOutput(expectedOutput, outputBytes, outputIsFile); err != nil {
				return nil, fmt.Errorf("Example %d: %s", index, err)
			}
		}

		setupTimes = append(setupTimes, result.SetupTime)
		runTimes = append(runTimes, result.RunTime)
		memoryUsages = append(memoryUsages, float64(result.UsedMemoryBytes))
		cpuUsages = append(cpuUsages, result.UsedCPUSecs)
	}

	if len(setupTimes) > 0 {
		if err := setAggregateStats(modelStats, setupTimes, runTimes, memoryUsages, cpuUsages); err != nil {
			return nil, err
		}
	}

	return &TestResult{
		RunArgs:           help.Arguments,
		Examples:          examples,
		NewExampleOutputs: newExampleOutputs,
		Stats:             modelStats,
	}, nil
}

func validateServingExampleInput(args map[string]*model.RunArgument, input map[string]string) error {
	// TODO(andreas): validate types
	missingNames := []string{}
	extraneousNames := []string{}

	for name, arg := range args {
		if _, ok := input[name]; !ok && arg.Default == nil {
			missingNames = append(missingNames, name)
		}
	}
	for name := range input {
		if _, ok := args[name]; !ok {
			extraneousNames = append(extraneousNames, name)
		}
	}
	errParts := []string{}
	if len(missingNames) > 0 {
		errParts = append(errParts, "Missing arguments: "+strings.Join(missingNames, ", "))
	}
	if len(extraneousNames) > 0 {
		errParts = append(errParts, "Extraneous arguments: "+strings.Join(extraneousNames, ", "))
	}
	if len(errParts) > 0 {
		return fmt.Errorf(strings.Join(errParts, "; "))
	}
	return nil
}

func extensionByType(mimeType string) string {
	switch mimeType {
	case "audio/aac":
		return ".aac"
	case "application/x-abiword":
		return ".abw"
	case "application/x-freearc":
		return ".arc"
	case "video/x-msvideo":
		return ".avi"
	case "application/vnd.amazon.ebook":
		return ".azw"
	case "application/octet-stream":
		return ".bin"
	case "image/bmp":
		return ".bmp"
	case "application/x-bzip":
		return ".bz"
	case "application/x-bzip2":
		return ".bz2"
	case "application/x-csh":
		return ".csh"
	case "text/css":
		return ".css"
	case "text/csv":
		return ".csv"
	case "application/msword":
		return ".doc"
	case "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		return ".docx"
	case "application/vnd.ms-fontobject":
		return ".eot"
	case "application/epub+zip":
		return ".epub"
	case "application/gzip":
		return ".gz"
	case "image/gif":
		return ".gif"
	case "text/html":
		return ".html"
	case "image/vnd.microsoft.icon":
		return ".ico"
	case "text/calendar":
		return ".ics"
	case "application/java-archive":
		return ".jar"
	case "image/jpeg":
		return ".jpg"
	case "text/javascript":
		return ".js"
	case "application/json":
		return ".json"
	case "application/ld+json":
		return ".jsonld"
	case "audio/midi audio/x-midi":
		return ".midi"
	case "audio/mpeg":
		return ".mp3"
	case "application/x-cdf":
		return ".cda"
	case "video/mp4":
		return ".mp4"
	case "video/mpeg":
		return ".mpeg"
	case "application/vnd.apple.installer+xml":
		return ".mpkg"
	case "application/vnd.oasis.opendocument.presentation":
		return ".odp"
	case "application/vnd.oasis.opendocument.spreadsheet":
		return ".ods"
	case "application/vnd.oasis.opendocument.text":
		return ".odt"
	case "audio/ogg":
		return ".oga"
	case "video/ogg":
		return ".ogv"
	case "application/ogg":
		return ".ogx"
	case "audio/opus":
		return ".opus"
	case "font/otf":
		return ".otf"
	case "image/png":
		return ".png"
	case "application/pdf":
		return ".pdf"
	case "application/x-httpd-php":
		return ".php"
	case "application/vnd.ms-powerpoint":
		return ".ppt"
	case "application/vnd.openxmlformats-officedocument.presentationml.presentation":
		return ".pptx"
	case "application/vnd.rar":
		return ".rar"
	case "application/rtf":
		return ".rtf"
	case "application/x-sh":
		return ".sh"
	case "image/svg+xml":
		return ".svg"
	case "application/x-shockwave-flash":
		return ".swf"
	case "application/x-tar":
		return ".tar"
	case "image/tiff":
		return ".tiff"
	case "video/mp2t":
		return ".ts"
	case "font/ttf":
		return ".ttf"
	case "text/plain":
		return ".txt"
	case "application/vnd.visio":
		return ".vsd"
	case "audio/wav":
		return ".wav"
	case "audio/webm":
		return ".weba"
	case "video/webm":
		return ".webm"
	case "image/webp":
		return ".webp"
	case "font/woff":
		return ".woff"
	case "font/woff2":
		return ".woff2"
	case "application/xhtml+xml":
		return ".xhtml"
	case "application/vnd.ms-excel":
		return ".xls"
	case "application/xml":
		return ".xml"
	case "application/zip":
		return ".zip"
	case "video/3gpp":
		return ".3gp"
	case "video/3gpp2":
		return ".3gp2"
	case "application/x-7z-compressed":
		return ".7z"
	default:
		extensions, _ := mime.ExtensionsByType(mimeType)
		if extensions == nil || len(extensions) == 0 {
			return ""
		}
		return extensions[0]
	}
}

func copyExamples(examples []*model.Example) []*model.Example {
	copy := []*model.Example{}
	for _, ex := range examples {
		inputCopy := map[string]string{}
		for k, v := range ex.Input {
			inputCopy[k] = v
		}
		copy = append(copy, &model.Example{
			Input:  inputCopy,
			Output: ex.Output,
		})
	}
	return copy
}

func outputBytesFromExample(exampleOutput string, dir string) (outputBytes []byte, outputIsFile bool, err error) {
	if exampleOutput != "" {
		if strings.HasPrefix(exampleOutput, "@") {
			outputBytes, err = os.ReadFile(filepath.Join(dir, exampleOutput[1:]))
			if err != nil {
				return nil, false, fmt.Errorf("Failed to read example output file %s: %w", exampleOutput[1:], err)
			}
			return outputBytes, true, nil
		} else {
			return []byte(exampleOutput), false, nil
		}
	}
	return nil, false, nil
}

func setAggregateStats(modelStats *model.Stats, setupTimes []float64, runTimes []float64, memoryUsages []float64, cpuUsages []float64) error {
	setupTime, err := stats.Mean(setupTimes)
	if err != nil {
		return err
	}
	modelStats.SetupTime = setupTime

	runTime, err := stats.Mean(runTimes)
	if err != nil {
		return err
	}
	modelStats.RunTime = runTime

	memoryUsage, err := stats.Max(memoryUsages)
	if err != nil {
		return err
	}
	modelStats.MemoryUsage = uint64(memoryUsage)

	cpuUsage, err := stats.Max(cpuUsages)
	if err != nil {
		return err
	}
	modelStats.CPUUsage = cpuUsage

	return nil
}

func updateExampleOutput(example *model.Example, newExampleOutputs map[string][]byte, outputBytes []byte, mimeType string, index int) {
	filename := fmt.Sprintf("output.%02d", index)
	if ext := extensionByType(mimeType); ext != "" {
		filename += ext
	}
	outputPath := filepath.Join(ExampleOutputDir, filename)
	example.Output = "@" + outputPath
	newExampleOutputs[outputPath] = outputBytes
}

func verifyCorrectOutput(expectedOutput []byte, outputBytes []byte, outputIsFile bool) error {
	if outputIsFile && !bytes.Equal(expectedOutput, outputBytes) {
		return fmt.Errorf("Output file contents doesn't match expected")
	} else if !outputIsFile && strings.TrimSpace(string(outputBytes)) != strings.TrimSpace(string(expectedOutput)) {
		// TODO(andreas): are there cases where space is significant?
		// TODO(andreas): truncate? diff?
		return fmt.Errorf("Output %s doesn't match expected: %s", string(outputBytes), string(expectedOutput))
	}
	return nil
}
