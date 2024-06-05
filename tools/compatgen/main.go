package main

import (
	"encoding/json"
	"os"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/tools/compatgen/internal"
)

func main() {
	var output string

	var rootCmd = &cobra.Command{
		Use:   "compatgen {cuda|torch|tensorflow}",
		Short: "Generate compatibility matrix for Cog base images",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			target := args[0]

			var v interface{}
			var err error

			switch target {
			case "cuda":
				v, err = internal.FetchCUDABaseImages()
				if err != nil {
					console.Fatalf("Failed to fetch CUDA base image tags: %s", err)
				}
			case "tensorflow":
				v, err = internal.FetchTensorFlowCompatibilityMatrix()
				if err != nil {
					console.Fatalf("Failed to fetch TensorFlow compatibility matrix: %s", err)
				}
			case "torch":
				v, err = internal.FetchTorchCompatibilityMatrix()
				if err != nil {
					console.Fatalf("Failed to fetch PyTorch compatibility matrix: %s", err)
				}
			default:
				console.Fatalf("Unknown target: %s", target)
			}

			data, err := json.MarshalIndent(v, "", "  ")
			if err != nil {
				console.Fatalf("Failed to marshal value: %s", err)
			}

			if output != "" {
				if err := os.WriteFile(output, data, 0o644); err != nil {
					console.Fatalf("Failed to write to %s: %s", output, err)
				}
				console.Infof("Wrote to %s", output)
			} else {
				console.Output(string(data))
			}
		},
	}

	rootCmd.Flags().StringVarP(&output, "output", "o", "", "Output flag (optional)")
	if err := rootCmd.Execute(); err != nil {
		console.Fatalf(err.Error())
	}
}
