package main

import (
	"context"
	"time"

	"github.com/replicate/cog/pkg/util/terminal"
)

func main() {
	ui := terminal.ConsoleUI(context.Background())
	defer ui.Close()
	status := ui.Status()
	status.Update("reticulating splines...")
	time.Sleep(2 * time.Second)
	status.Step(terminal.StatusOK, "hello world")
}
