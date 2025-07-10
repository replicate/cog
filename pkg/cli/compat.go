package cli

import (
	"fmt"
	"os"

	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/base_images"
)

func newCompatCommand() *cobra.Command {
	opts := compatOptions{}

	cmd := &cobra.Command{
		Use:   "compat",
		Short: "Inspect compatibility matrix",
		RunE:  opts.run,
	}

	cmd.Flags().StringVar(&opts.pythonVersion, "python", "", "Python version")
	cmd.Flags().StringVar(&opts.cudaVersion, "cuda", "", "CUDA version")

	return cmd
}

type compatOptions struct {
	pythonVersion string
	cudaVersion   string
}

func (o *compatOptions) run(cmd *cobra.Command, args []string) error {
	fmt.Println("Compat")

	constraints := []base_images.Constraint{}

	if o.pythonVersion != "" {
		constraints = append(constraints, base_images.PythonConstraint(o.pythonVersion))
	}

	if o.cudaVersion != "" {
		constraints = append(constraints, base_images.CudaConstraint(o.cudaVersion))
	}

	images, err := base_images.Query(constraints...)
	if err != nil {
		return err
	}

	tw := tablewriter.NewWriter(os.Stdout)
	tw.Header("Accelerator", "Python", "Ubuntu", "Run Tag", "Dev Tag")
	for _, image := range images {
		var accelerator string
		if image.Accelerator == base_images.AcceleratorCPU {
			accelerator = "CPU"
		} else {
			accelerator = fmt.Sprintf("CUDA %s", image.CudaVersion.String())
			if image.CuDNN {
				accelerator += " CuDNN)"
			}
		}

		_ = tw.Append([]string{accelerator, image.PythonVersion.String(), image.UbuntuVersion.String(), image.RunTag, image.DevTag})
	}
	return tw.Render()
}
