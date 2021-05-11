package main

import (
	"context"
	"fmt"
	"time"

	"github.com/replicate/cog/pkg/util/terminal"
)

func main() {
	ui := terminal.ConsoleUI(context.Background())
	defer ui.Close()
	sg := ui.StepGroup()
	defer sg.Wait()

	// https://gist.github.com/erikcox/7e96d031d00d7ecb1a2f

	s1 := sg.Add("Reticulating Splines...")
	s2 := sg.Add("Coalescing Cloud Formations")
	s3 := sg.Add("Destabilizing Economic Indicators")

	i := 0
	go func() {
		for {
			fmt.Fprintf(s1.TermOutput(), "reticulating spline %d\n", i)
			i++
			time.Sleep(5 * time.Millisecond)
		}
	}()

	time.Sleep(3 * time.Second)

	s3.Done()
	time.Sleep(1 * time.Second)
	s2.Done()
	time.Sleep(1 * time.Second)
	s1.Done()
	// or s1.Abort() on error and it'll print the whole scrollback
}
