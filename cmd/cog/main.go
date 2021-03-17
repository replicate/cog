package main

import (
	"github.com/replicate/modelserver/pkg/cli"

	log "github.com/sirupsen/logrus"
)

func main() {
	cmd, err := cli.NewRootCommand()
	if err != nil {
		log.Fatalf("%f", err)
	}

	if err = cmd.Execute(); err != nil {
		log.Fatalf("%s", err)
	}
}
