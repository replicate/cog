package main

import (
	"log"
	"github.com/replicate/cog/pkg/cli"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/spf13/cobra/doc"
)

func main() {
	cmd, err := cli.NewRootCommand()
	if err != nil {
		console.Fatalf("%f", err)
	}

	err = doc.GenMarkdownTree(cmd, "./docs/tmp")
	if err != nil {
		log.Fatal(err)
	}

	if err = cmd.Execute(); err != nil {
		console.Fatalf("%s", err)
	}
}
