package cli

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/xeonx/timeago"

	"github.com/replicate/cog/pkg/client"
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

	return cmd
}

func showModel(cmd *cobra.Command, args []string) error {
	repo, err := getRepo()
	if err != nil {
		return err
	}

	id := args[0]

	cli := client.NewClient()
	mod, err := cli.GetModel(repo, id)
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID:\t"+mod.ID)
	fmt.Fprintf(w, "Repo:\t%s/%s\n", repo.User, repo.Name)
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
			if arg.Min != nil {
				typeStr += ", min: " + *arg.Min
			}
			if arg.Max != nil {
				typeStr += ", min: " + *arg.Max
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
