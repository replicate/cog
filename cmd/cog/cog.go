package main

import (
	"github.com/replicate/cog/pkg/cli"

	"github.com/replicate/cog/pkg/console"
)

func main() {
	cmd, err := cli.NewRootCommand()
	if err != nil {
		console.Fatal("%f", err)
	}

	if err = cmd.Execute(); err != nil {
		console.Fatal("%s", err)
	}
}
