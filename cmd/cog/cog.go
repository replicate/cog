package main

import (
	"github.com/replicate/cog/pkg/cli"
	"github.com/replicate/cog/pkg/util/console"
)

func main() {
	cmd, err := cli.NewRootCommand()
	if err != nil {
		console.Fatalf("%f", err)
	}

	if err = cmd.Execute(); err != nil {
		console.Fatalf("%s", err)
	}
}
