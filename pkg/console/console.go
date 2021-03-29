// Package console provides a standard interface for user- and machine-interface with the console
package console

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/logrusorgru/aurora"
	"github.com/mitchellh/go-wordwrap"
)

// Console represents a standardized interface for console UI. It is designed to abstract:
// - Writing messages to logs or displaying on console
// - Console user interface elements (progress, interactive prompts, etc)
// - Switching between human and machine modes for these things (e.g. don't display progress bars or colors in logs, don't prompt for input when in a script)
type Console struct {
	Color     bool
	IsMachine bool
	Level     Level
	mu        sync.Mutex
}

// Debug level message
func (c *Console) Debug(msg string, v ...interface{}) {
	c.log(DebugLevel, msg, v...)
}

// Info level message
func (c *Console) Info(msg string, v ...interface{}) {
	c.log(InfoLevel, msg, v...)
}

// Warn level message
func (c *Console) Warn(msg string, v ...interface{}) {
	c.log(WarnLevel, msg, v...)
}

// Error level message
func (c *Console) Error(msg string, v ...interface{}) {
	c.log(ErrorLevel, msg, v...)
}

// Fatal level message, followed by exit
func (c *Console) Fatal(msg string, v ...interface{}) {
	c.log(FatalLevel, msg, v...)
	os.Exit(1)
}

// Output a line to stdout. Useful for printing primary output of a command, or the output of a subcommand.
func (c *Console) Output(line string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fmt.Fprintln(os.Stdout, line)
}

// OutputErr a line to stderr. Useful for printing primary output of a command, or the output of a subcommand.
func (c *Console) OutputErr(line string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fmt.Fprintln(os.Stderr, line)
}

// DebugOutput a line to stdout. Like Output, but only when level is DebugLevel.
func (c *Console) DebugOutput(line string) {
	if c.Level > DebugLevel {
		return
	}
	if c.Color {
		line = aurora.Faint(line).String()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	fmt.Fprintln(os.Stderr, line)
}

func (c *Console) log(level Level, msg string, v ...interface{}) {
	if level < c.Level {
		return
	}

	prompt := "═══╡ "
	continuationPrompt := "   │ "

	formattedMsg := fmt.Sprintf(msg, v...)

	// Word wrap
	width, err := GetWidth()
	if err != nil {
		Debug("error getting width of terminal: %s", err)
	} else if width > 30 {
		// Only do word wrapping for terminals 30 chars or wider. Anything smaller and the terminal
		// is probably resized really small for some reason, and when the user resizes it to be big
		// again then we want the text to flow sensibly instead of having one word per line, or
		// whatever. This also makes width-len(prompt) always be positive.
		formattedMsg = wordwrap.WrapString(formattedMsg, uint(width)-uint(len(prompt)))
	}

	// Add color after word wrapping so naive length of prompt is correct
	if c.Color {
		color := aurora.Faint
		if level == WarnLevel {
			color = aurora.Yellow
		} else if level == ErrorLevel {
			color = aurora.Red
		} else if level == FatalLevel {
			color = aurora.Red
		}
		prompt = color(prompt).String()
		continuationPrompt = color(continuationPrompt).String()
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for i, line := range strings.Split(formattedMsg, "\n") {
		if c.Color && level == DebugLevel {
			line = aurora.Faint(line).String()
		}
		if i == 0 {
			line = prompt + line
		} else {
			line = continuationPrompt + line
		}
		fmt.Fprintln(os.Stderr, line)
	}
}
