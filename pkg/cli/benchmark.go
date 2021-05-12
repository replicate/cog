package cli

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/montanaflynn/stats"
	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/client"
	"github.com/replicate/cog/pkg/logger"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/serving"
	"github.com/replicate/cog/pkg/util/console"
)

var benchmarkSetups int
var benchmarkRuns int
var benchmarkArch string

type BenchmarkResults struct {
	SetupTimes []float64
	RunTimes   []float64
	BootTimes  []float64
}

func newBenchmarkCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "benchmark",
		Short: "Measure setup and runtime of model, using the first example from config",
		RunE:  benchmarkModel,
		Args:  cobra.ExactArgs(1),
	}

	cmd.Flags().IntVarP(&benchmarkSetups, "setup-iterations", "s", 3, "Number of setup iterations")
	cmd.Flags().IntVarP(&benchmarkRuns, "run-iterations", "r", 3, "Number of run iterations per setup iteration")
	cmd.Flags().StringVarP(&benchmarkArch, "arch", "a", "cpu", "Architecture to benchmark (cpu/gpu)")

	return cmd
}

func benchmarkModel(cmd *cobra.Command, args []string) error {
	mod, err := getModel()
	if err != nil {
		return err
	}
	id := args[0]

	cli := client.NewClient()
	console.Infof("Starting benchmark of %s:%s", mod, id)

	version, images, err := cli.GetVersion(mod, id)
	if err != nil {
		return err
	}
	image := model.ImageForArch(images, benchmarkArch)
	if image == nil {
		return fmt.Errorf("No %s image has been built for %s:%s", benchmarkArch, mod.String(), id)
	}

	if len(version.Config.Examples) == 0 {
		return fmt.Errorf("Model has no examples, cannot run benchmark")
	}

	tmpDir, err := os.MkdirTemp("/tmp", "benchmark")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	if err := cli.DownloadVersion(mod, id, tmpDir); err != nil {
		return err
	}
	results := new(BenchmarkResults)
	for i := 0; i < benchmarkSetups; i++ {
		console.Infof("Running setup iteration %d", i+1)
		if err := runBenchmarkInference(version, image, tmpDir, results, benchmarkRuns); err != nil {
			return err
		}
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "Total setups:\t%d\n", benchmarkSetups)
	fmt.Fprintf(w, "Total runs:\t%d\n", benchmarkSetups*benchmarkRuns)
	w.Flush()

	w = tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "STEP\tAVERAGE\tMIN\tMAX")
	averageBoot, minBoot, maxBoot := benchmarkStats(results.BootTimes)
	fmt.Fprintf(w, "Boot\t%.3f\t%.3f\t%.3f\n", averageBoot, minBoot, maxBoot)
	averageSetup, minSetup, maxSetup := benchmarkStats(results.SetupTimes)
	fmt.Fprintf(w, "Setup\t%.3f\t%.3f\t%.3f\n", averageSetup, minSetup, maxSetup)
	averageRun, minRun, maxRun := benchmarkStats(results.RunTimes)
	fmt.Fprintf(w, "Run\t%.3f\t%.3f\t%.3f\n", averageRun, minRun, maxRun)
	w.Flush()
	return nil
}

func runBenchmarkInference(version *model.Version, image *model.Image, modelDir string, results *BenchmarkResults, runIterations int) error {
	servingPlatform, err := serving.NewLocalDockerPlatform()
	if err != nil {
		return err
	}

	example := version.Config.Examples[0]
	input := serving.NewExampleWithBaseDir(example.Input, modelDir)

	logWriter := logger.NewConsoleLogger()
	bootStart := time.Now()
	deployment, err := servingPlatform.Deploy(context.Background(), image.URI, benchmarkArch == "docker-gpu", logWriter)
	if err != nil {
		return err
	}
	defer func() {
		if err := deployment.Undeploy(); err != nil {
			console.Warnf("Failed to kill Docker container: %s", err)
		}
	}()
	bootTime := time.Since(bootStart).Seconds()
	results.BootTimes = append(results.BootTimes, bootTime)

	for i := 0; i < runIterations; i++ {
		result, err := deployment.RunInference(context.Background(), input, logWriter)
		if err != nil {
			return err
		}
		if i == 0 {
			results.SetupTimes = append(results.SetupTimes, result.SetupTime)
		}
		results.RunTimes = append(results.RunTimes, result.RunTime)
	}

	return nil
}

func benchmarkStats(values []float64) (mean, min, max float64) {
	var err error
	mean, err = stats.Mean(values)
	if err != nil {
		panic(err)
	}
	min, err = stats.Min(values)
	if err != nil {
		panic(err)
	}
	max, err = stats.Max(values)
	if err != nil {
		panic(err)
	}
	return mean, min, max
}
