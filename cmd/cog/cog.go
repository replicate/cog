package main

import (
	"errors"
	"os"

	"github.com/replicate/cog/pkg/cli"
	"github.com/replicate/cog/pkg/util/console"
)

func main() {
	cmd, err := cli.NewRootCommand()
	if err != nil {
		console.Fatalf("%f", err)
	}

	if err = cmd.Execute(); err != nil {
		if errors.Is(err, cli.ErrScanFindings) {
			console.Errorf("%s", err)
			os.Exit(exitCode(err))
		}
		console.Fatalf("%s", err)
	}
}

func exitCode(err error) int {
	if errors.Is(err, cli.ErrScanFindings) {
		return 2
	}
	return 1
}
