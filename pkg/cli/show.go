package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/xeonx/timeago"

	"github.com/replicate/cog/pkg/model"
)

func newShowCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <id>",
		Short: "Inspect a Cog package",
		RunE:  showPackage,
		Args:  cobra.ExactArgs(1),
	}

	cmd.Flags().StringVarP(&buildHost, "build-host", "H", "127.0.0.1:8080", "address to the build host")

	return cmd
}

func showPackage(cmd *cobra.Command, args []string) error {
	id := args[0]

	resp, err := http.Get("http://" + buildHost + "/v1/packages/" + id)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Show endpoint returned status %d", resp.StatusCode)
	}

	mod := new(model.Model)
	if err := json.NewDecoder(resp.Body).Decode(mod); err != nil {
		return fmt.Errorf("Failed to decode response: %w", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID:\t"+mod.ID)
	fmt.Fprintln(w, "Name:\t"+mod.Name)
	fmt.Fprintln(w, "Created:\t"+timeago.English.Format(mod.Created))
	w.Flush()

	fmt.Println()

	fmt.Println("Inference arguments:")
	if len(mod.RunArguments) > 0 {
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		for name, arg := range mod.RunArguments {
			typeStr := string(arg.Type)
			help := ""
			if arg.Help != nil {
				help = *arg.Help
			}
			if arg.Default != nil {
				typeStr += ", default: " + *arg.Default
			}
			fmt.Fprintf(w, "* %s\t(%s)\t%s\n", name, typeStr, help)
		}
		w.Flush()
	} else {
		fmt.Println("  [No arguments]")
	}
	fmt.Println()

	fmt.Println("Artifacts:")
	if len(mod.Artifacts) > 0 {
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		for _, artifact := range mod.Artifacts {
			fmt.Fprintf(w, "* %s:\t%s\n", artifact.Target, artifact.URI)
		}
		w.Flush()
	} else {
		fmt.Println("  [No artifacts]")
	}
	fmt.Println()

	env := mod.Config.Environment
	fmt.Println("Python version: " + env.PythonVersion)
	fmt.Println()
	if env.CUDA != "" {
		fmt.Println("CUDA version:  " + env.CUDA)
		fmt.Println("CuDNN version: " + env.CuDNN)
		fmt.Println()
	}

	if len(env.PythonPackages) > 0 {
		fmt.Println("Python packages:")
		for _, pkg := range env.PythonPackages {
			fmt.Println("* " + pkg)
		}
		fmt.Println()
	}
	if len(env.PythonFindLinks) > 0 {
		fmt.Println("Python --find-links:")
		for _, url := range env.PythonFindLinks {
			fmt.Println("* " + url)
		}
		fmt.Println()
	}
	if len(env.PythonExtraIndexURLs) > 0 {
		fmt.Println("Python --extra-index-urls:")
		for _, url := range env.PythonExtraIndexURLs {
			fmt.Println("* " + url)
		}
		fmt.Println()
	}
	if env.PythonRequirements != "" {
		fmt.Println("Python requirements file: " + env.PythonRequirements)
		fmt.Println()
	}
	if len(env.SystemPackages) > 0 {
		fmt.Println("System packages:")
		for _, pkg := range env.SystemPackages {
			fmt.Println("* " + pkg)
		}
		fmt.Println()
	}

	return nil
}
