package cli

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/montanaflynn/stats"
	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/client"
	"github.com/replicate/cog/pkg/console"
	"github.com/replicate/cog/pkg/logger"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/serving"
)

var benchmarkSetups int
var benchmarkRuns int

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

	return cmd
}

func benchmarkModel(cmd *cobra.Command, args []string) error {
	repo, err := getRepo()
	if err != nil {
		return err
	}
	id := args[0]

	cli := client.NewClient()
	console.Info("Starting benchmark of %s:%s", repo, id)

	mod, err := cli.GetModel(repo, id)
	if err != nil {
		return err
	}
	if len(mod.Config.Examples) == 0 {
		return fmt.Errorf("Model has no examples, cannot run benchmark")
	}

	modelDir, err := os.MkdirTemp("/tmp", "benchmark")
	if err != nil {
		return err
	}
	defer os.RemoveAll(modelDir)
	if err := cli.DownloadModel(repo, id, modelDir); err != nil {
		return err
	}
	results := new(BenchmarkResults)
	for i := 0; i < benchmarkSetups; i++ {
		console.Info("Running setup iteration %d", i+1)
		if err := runBenchmarkInference(mod, modelDir, results, benchmarkRuns); err != nil {
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

func runBenchmarkInference(mod *model.Model, modelDir string, results *BenchmarkResults, runIterations int) error {
	servingPlatform, err := serving.NewLocalDockerPlatform()
	if err != nil {
		return err
	}

	example := mod.Config.Examples[0]
	input := serving.NewExampleWithBaseDir(example.Input, modelDir)

	logWriter := logger.NewConsoleLogger()
	bootStart := time.Now()
	deployment, err := servingPlatform.Deploy(mod, model.TargetDockerCPU, logWriter)
	if err != nil {
		return err
	}
	defer func() {
		if err := deployment.Undeploy(); err != nil {
			console.Warn("Failed to kill Docker container: %s", err)
		}
	}()
	bootTime := time.Since(bootStart).Seconds()
	results.BootTimes = append(results.BootTimes, bootTime)

	for i := 0; i < runIterations; i++ {
		result, err := deployment.RunInference(input, logWriter)
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
