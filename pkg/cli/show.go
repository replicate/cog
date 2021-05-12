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
		Short:      "Inspect a Cog model",
		RunE:       showModel,
		Args:       cobra.ExactArgs(1),
		SuggestFor: []string{"inspect"},
	}
	addRepoFlag(cmd)

	cmd.Flags().Bool("json", false, "JSON output")

	return cmd
}

func showModel(cmd *cobra.Command, args []string) error {
	jsonOutput, err := cmd.Flags().GetBool("json")
	if err != nil {
		return err
	}
	repo, err := getRepo()
	if err != nil {
		return err
	}

	id := args[0]

	cli := client.NewClient()
	mod, images, err := cli.GetModel(repo, id)
	if err != nil {
		return err
	}

	if jsonOutput {
		return showModelJSON(mod, images)
	}
	return showModelTable(repo, mod, images)
}

func showModelJSON(mod *model.Model, images []*model.Image) error {
	output := struct{
		Model *model.Model `json:"model"`
		Images []*model.Image `json:"images"`
	}{
		Model: mod,
		Images: images,
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func showModelTable(repo *model.Repo, mod *model.Model, images []*model.Image) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID:\t"+mod.ID)
	fmt.Fprintf(w, "Repo:\t%s/%s\n", repo.User, repo.Name)
	fmt.Fprintln(w, "Created:\t"+timeago.English.Format(mod.Created))
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
	if len(mod.Config.Environment.Architectures) == 0 {
		fmt.Println("  [No images]")
	} else {
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		for _, arch := range mod.Config.Environment.Architectures {
			image := model.ImageForArch(images, arch)
			var message string
			switch {
			case image == nil:
				message = fmt.Sprintf("Building... See the status with 'cog build log %s'", mod.BuildIDs[arch])
			case image.BuildFailed:
				message = fmt.Sprintf("Build failed. See the build logs with 'cog build log %s'", mod.BuildIDs[arch])
			default:
				message = image.URI
			}
			fmt.Fprintf(w, "%s:\t%s\n", strings.ToUpper(arch), message)
		}
		w.Flush()
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
