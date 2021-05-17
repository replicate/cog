package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/TylerBrock/colorjson"
	"github.com/mitchellh/go-homedir"
	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/client"
	"github.com/replicate/cog/pkg/logger"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/serving"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/pkg/util/mime"
	"github.com/replicate/cog/pkg/util/slices"
)

type inferFileLine struct {
	number int
	fields []string
}

var (
	inputs    []string
	inputFile string
	outPath   string
	outputDir string
	inferArch string
)

func newInferCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "infer <id>",
		Short: "Run a single inference against a version of a model",
		RunE:  cmdInfer,
		Args:  cobra.MinimumNArgs(1),
	}
	addModelFlag(cmd)
	cmd.Flags().StringArrayVarP(&inputs, "input", "i", []string{}, "Inputs, in the form name=value. if value is prefixed with @, then it is read from a file on disk. E.g. -i path=@image.jpg")
	cmd.Flags().StringVar(&inputFile, "file", "", "File containing inputs in the format key1=val1,key2=\"val2\", etc., one input per line. If value is prefixed with @, then it is read from a file on disk. E.g. key1=val1,key2=my-dir/image.jpg")
	cmd.Flags().StringVarP(&outPath, "output", "o", "", "Output path")
	cmd.Flags().StringVar(&outputDir, "output-dir", "", "Output directory for when --file is used. Output files are named output.XXXXXXX.EXT, where XXXXXXX corresponds to the line in the input file, starting with 0000001.")
	cmd.Flags().StringVarP(&inferArch, "arch", "a", "cpu", "Architecture to run inference on (cpu/gpu)")

	return cmd
}

func cmdInfer(cmd *cobra.Command, args []string) error {
	if len(inputs) > 0 && inputFile != "" {
		return fmt.Errorf("--input and --file are mutually exclusive. Please only use one or the other")
	}
	if outPath != "" && outputDir != "" {
		return fmt.Errorf("--output and --output-dir are mutually exclusive. Please only use one or the other")
	}
	if len(inputs) > 0 && outputDir != "" {
		return fmt.Errorf("--output-dir can only be used in conjunction with --file")
	}
	if inputFile != "" && outPath != "" {
		return fmt.Errorf("--output can only be used in conjunction with --input")
	}
	if !slices.ContainsString([]string{"cpu", "gpu"}, inferArch) {
		return fmt.Errorf("--arch must be either 'cpu' or 'gpu'")
	}

	mod, err := getModel()
	if err != nil {
		return err
	}

	id := args[0]

	client := client.NewClient()
	fmt.Println("Loading package", id)
	version, err := client.GetVersion(mod, id)
	if err != nil {
		return err
	}
	// TODO(bfirsh): differentiate between failed builds and in-progress builds, and probably block here if there is an in-progress build
	image := model.ImageForArch(version.Images, benchmarkArch)
	if image == nil {
		return fmt.Errorf("No %s image has been built for %s:%s", benchmarkArch, mod.String(), id)
	}

	servingPlatform, err := serving.NewLocalDockerPlatform()
	if err != nil {
		return err
	}
	logWriter := logger.NewConsoleLogger()
	useGPU := inferArch == "gpu"
	deployment, err := servingPlatform.Deploy(context.Background(), image.URI, useGPU, logWriter)
	if err != nil {
		return err
	}
	defer func() {
		if err := deployment.Undeploy(); err != nil {
			console.Warnf("Failed to kill Docker container: %s", err)
		}
	}()

	if len(inputs) > 0 {
		return inferIndividualInputs(deployment, inputs, outPath, logWriter)
	}
	return inferBatch(deployment, inputFile, outputDir, logWriter)
}

func inferIndividualInputs(deployment serving.Deployment, inputs []string, outputPath string, logWriter logger.Logger) error {
	example := parseInferInputs(inputs)
	result, err := deployment.RunInference(context.Background(), example, logWriter)
	if err != nil {
		return err
	}
	// TODO(andreas): support multiple outputs?
	output := result.Values["output"]

	// Write to stdout
	if outputPath == "" {
		// Is it something we can sensibly write to stdout?
		if output.MimeType == "plain/text" {
			_, err := io.Copy(os.Stdout, output.Buffer)
			return err
		} else if output.MimeType == "application/json" {
			var obj map[string]interface{}
			dec := json.NewDecoder(output.Buffer)
			if err := dec.Decode(&obj); err != nil {
				return err
			}
			f := colorjson.NewFormatter()
			f.Indent = 2
			s, _ := f.Marshal(obj)
			fmt.Println(string(s))
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

	fmt.Println("Written output to " + outputPath)
	return nil
}

func inferBatch(deployment serving.Deployment, inputFile string, outputDir string, logWriter logger.Logger) error {
	file, err := os.Open(inputFile)
	if err != nil {
		return fmt.Errorf("Failed to open %s: %w", inputFile, err)
	}
	defer file.Close()
	for line := range iterateInferFileLines(file) {
		example := parseInferInputs(line.fields)
		result, err := deployment.RunInference(context.Background(), example, logWriter)
		if err != nil {
			console.Errorf("Failed to process line %d: %v", line.number, err)
			continue
		}
		output := result.Values["output"]
		outputFilename := fmt.Sprintf("output.%07d", line.number)
		extension := mime.ExtensionByType(output.MimeType)
		if extension != "" {
			outputFilename += extension
		}
		outputPath := filepath.Join(outputDir, outputFilename)
		outFile, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE, 0755)
		if err != nil {
			return err
		}
		if _, err := io.Copy(outFile, output.Buffer); err != nil {
			return err
		}
	}
	return nil
}

func iterateInferFileLines(reader io.Reader) <-chan *inferFileLine {
	ch := make(chan *inferFileLine)
	scanner := bufio.NewScanner(reader)
	go func() {
		defer close(ch)
		for lineNumber := 1; scanner.Scan(); lineNumber++ {
			fields := commaSplit(scanner.Text())
			// allow empty lines
			if len(fields) == 0 {
				continue
			}
			// allow # prefix for comments
			if strings.HasPrefix(fields[0], "#") {
				continue
			}
			ch <- &inferFileLine{number: lineNumber, fields: fields}
		}
	}()
	return ch
}

// split a line like `a, b ,"c,d"` into []{"a", "b", `"c,d"`}
func commaSplit(line string) []string {
	quoted := false
	fields := strings.FieldsFunc(line, func(r rune) bool {
		if r == '"' {
			quoted = !quoted
		}
		return !quoted && r == ','
	})
	for i, field := range fields {
		fields[i] = strings.TrimSpace(field)
	}
	return fields
}

func parseInferInputs(inputs []string) *serving.Example {
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
