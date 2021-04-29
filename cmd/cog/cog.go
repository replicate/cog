package main

import (
	"github.com/replicate/cog/pkg/cli"
	"github.com/replicate/cog/pkg/console"
	"github.com/replicate/cog/pkg/global"
)

func main() {
	cmd, err := cli.NewRootCommand()
	if err != nil {
		console.Fatalf("%f", err)
	}
	defer func() {
		if global.Profiler != nil {
			global.Profiler.Stop()
		}
	}()

	if err = cmd.Execute(); err != nil {
		console.Fatalf("%s", err)
	}
}
