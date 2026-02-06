package main

import (
	kongcli "github.com/replicate/cog/pkg/cli/kong"
	"github.com/replicate/cog/pkg/util/console"
)

func main() {
	if err := kongcli.Execute(); err != nil {
		console.Fatalf("%s", err)
	}
}
