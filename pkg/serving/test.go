package serving

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/montanaflynn/stats"
	"github.com/replicate/cog/pkg/logger"
	"github.com/replicate/cog/pkg/model"
)

func TestModel(servingPlatform Platform, imageTag string, config *model.Config, dir string, logWriter logger.Logger) (map[string]*model.RunArgument, *model.Stats, error) {
	logWriter.WriteStatus("Testing model")

	modelStats := new(model.Stats)

	bootStart := time.Now()
	deployment, err := servingPlatform.Deploy(imageTag, logWriter)
	if err != nil {
		return nil, nil, err
	}
	defer deployment.Undeploy()

	modelStats.BootTime = time.Since(bootStart).Seconds()

	help, err := deployment.Help(logWriter)
	if err != nil {
		return nil, nil, err
	}

	setupTimes := []float64{}
	runTimes := []float64{}
	memoryUsages := []float64{}
	for _, example := range config.Examples {
		if err := validateServingExampleInput(help, example.Input); err != nil {
			return nil, nil, fmt.Errorf("Example input doesn't match run arguments: %w", err)
		}
		var expectedOutput []byte = nil
		outputIsFile := false
		if example.Output != "" {
			if strings.HasPrefix(example.Output, "@") {
				outputIsFile = true
				expectedOutput, err = os.ReadFile(filepath.Join(dir, config.Workdir, example.Output[1:]))
				if err != nil {
					return nil, nil, fmt.Errorf("Failed to read example output file %s: %w", example.Output[1:], err)
				}
			} else {
				expectedOutput = []byte(example.Output)
			}
		}

		input := NewExampleWithBaseDir(example.Input, filepath.Join(dir, config.Workdir))

		result, err := deployment.RunInference(input, logWriter)
		if err != nil {
			return nil, nil, err
		}
		output := result.Values["output"]
		outputBytes, err := io.ReadAll(output.Buffer)
		if err != nil {
			return nil, nil, fmt.Errorf("Failed to read output: %w", err)
		}
		logWriter.Infof(fmt.Sprintf("Inference result length: %d, mime type: %s", len(outputBytes), output.MimeType))
		if expectedOutput != nil {
			if outputIsFile && !bytes.Equal(expectedOutput, outputBytes) {
				return nil, nil, fmt.Errorf("Output file contents doesn't match expected %s", example.Output[1:])
			} else if !outputIsFile && strings.TrimSpace(string(outputBytes)) != strings.TrimSpace(example.Output) {
				// TODO(andreas): are there cases where space is significant?
				return nil, nil, fmt.Errorf("Output %s doesn't match expected: %s", string(outputBytes), example.Output)
			}
		}

		setupTimes = append(setupTimes, result.SetupTime)
		runTimes = append(runTimes, result.RunTime)
		memoryUsages = append(memoryUsages, float64(result.MemoryUsage))
	}

	if len(setupTimes) > 0 {
		modelStats.SetupTime, err = stats.Mean(setupTimes)
		if err != nil {
			return nil, nil, err
		}
		modelStats.RunTime, err = stats.Mean(setupTimes)
		if err != nil {
			return nil, nil, err
		}
		memoryUsage, err := stats.Max(memoryUsages)
		if err != nil {
			return nil, nil, err
		}
		modelStats.MemoryUsage = uint64(memoryUsage)
	} else {
		modelStats.SetupTime = 0
		modelStats.RunTime = 0
		modelStats.MemoryUsage = 0
	}

	return help.Arguments, modelStats, nil
}

func validateServingExampleInput(help *HelpResponse, input map[string]string) error {
	// TODO(andreas): validate types
	missingNames := []string{}
	extraneousNames := []string{}

	for name, arg := range help.Arguments {
		if _, ok := input[name]; !ok && arg.Default == nil {
			missingNames = append(missingNames, name)
		}
	}
	for name := range input {
		if _, ok := help.Arguments[name]; !ok {
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
