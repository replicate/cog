package main

import (
	"github.com/sieve-data/cog/pkg/cli"
	"github.com/sieve-data/cog/pkg/util/console"
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
