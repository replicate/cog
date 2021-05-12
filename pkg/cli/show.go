package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/xeonx/timeago"

	"github.com/replicate/cog/pkg/client"
	"github.com/replicate/cog/pkg/model"
)

func newShowCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:        "show <id>",
		Short:      "Show detailed information about a version",
		RunE:       show,
		Args:       cobra.ExactArgs(1),
		SuggestFor: []string{"inspect"},
	}
	addModelFlag(cmd)

	cmd.Flags().Bool("json", false, "Print information as JSON")

	return cmd
}

func show(cmd *cobra.Command, args []string) error {
	jsonOutput, err := cmd.Flags().GetBool("json")
	if err != nil {
		return err
	}
	model, err := getModel()
	if err != nil {
		return err
	}

	id := args[0]

	cli := client.NewClient()
	version, images, err := cli.GetVersion(model, id)
	if err != nil {
		return err
	}

	if jsonOutput {
		return showJSON(version, images)
	}
	return showTable(model, version, images)
}

func showJSON(version *model.Version, images []*model.Image) error {
	output := struct {
		Model  *model.Version `json:"version"`
		Images []*model.Image `json:"images"`
	}{
		Model:  version,
		Images: images,
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func showTable(mod *model.Model, version *model.Version, images []*model.Image) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID:\t"+version.ID)
	fmt.Fprintf(w, "Model:\t%s/%s\n", mod.User, mod.Name)
	fmt.Fprintln(w, "Created:\t"+timeago.English.Format(version.Created))
	w.Flush()

	fmt.Println()

	fmt.Println("Inference arguments:")
	if len(images) > 0 && images[0] != nil && images[0].RunArguments != nil && len(images[0].RunArguments) > 0 {
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		for name, arg := range images[0].RunArguments {
			typeStr := string(arg.Type)
			help := ""
			if arg.Help != nil {
				help = *arg.Help
			}
			if arg.Min != nil {
				typeStr += ", min: " + *arg.Min
			}
			if arg.Max != nil {
				typeStr += ", max: " + *arg.Max
			}
			if arg.Options != nil {
				typeStr += ", options: {" + strings.Join(*arg.Options, ", ") + "}"
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

	fmt.Println("Images:")
	if len(version.Config.Environment.Architectures) == 0 {
		fmt.Println("  [No images]")
	} else {
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		for _, arch := range version.Config.Environment.Architectures {
			image := model.ImageForArch(images, arch)
			var message string
			switch {
			case image == nil:
				message = fmt.Sprintf("Building... See the status with 'cog build log %s'", version.BuildIDs[arch])
			case image.BuildFailed:
				message = fmt.Sprintf("Build failed. See the build logs with 'cog build log %s'", version.BuildIDs[arch])
			default:
				message = image.URI
			}
			fmt.Fprintf(w, "%s:\t%s\n", strings.ToUpper(arch), message)
		}
		w.Flush()
	}
	fmt.Println()

	env := version.Config.Environment
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
