// Package console provides a standard interface for user- and machine-interface with the console
package console

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/logrusorgru/aurora"
)

// Console represents a standardized interface for console UI. It is designed to abstract:
// - Writing main output
// - Giving information to user
// - Console user interface elements (progress, interactive prompts, etc)
// - Switching between human and machine modes for these things (e.g. don't display progress bars or colors in logs, don't prompt for input when in a script)
type Console struct {
	Color     bool
	IsMachine bool
	Level     Level
	mu        sync.Mutex
}

// Debug prints a verbose debugging message, that is not displayed by default to the user.
func (c *Console) Debug(msg string) {
	c.log(DebugLevel, msg)
}

// Info tells the user what's going on.
func (c *Console) Info(msg string) {
	c.log(InfoLevel, msg)
}

// Warn tells the user that something might break.
func (c *Console) Warn(msg string) {
	c.log(WarnLevel, msg)
}

// Error tells the user that something is broken.
func (c *Console) Error(msg string) {
	c.log(ErrorLevel, msg)
}

// Fatal level message, followed by exit
func (c *Console) Fatal(msg string) {
	c.log(FatalLevel, msg)
	os.Exit(1)
}

// Debug level message
func (c *Console) Debugf(msg string, v ...interface{}) {
	c.log(DebugLevel, fmt.Sprintf(msg, v...))
}

// Info level message
func (c *Console) Infof(msg string, v ...interface{}) {
	c.log(InfoLevel, fmt.Sprintf(msg, v...))
}

// Warn level message
func (c *Console) Warnf(msg string, v ...interface{}) {
	c.log(WarnLevel, fmt.Sprintf(msg, v...))
}

// Error level message
func (c *Console) Errorf(msg string, v ...interface{}) {
	c.log(ErrorLevel, fmt.Sprintf(msg, v...))
}

// Fatal level message, followed by exit
func (c *Console) Fatalf(msg string, v ...interface{}) {
	c.log(FatalLevel, fmt.Sprintf(msg, v...))
	os.Exit(1)
}

// Output a string to stdout. Useful for printing primary output of a command, or the output of a subcommand.
// A newline is added to the string.
func (c *Console) Output(s string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fmt.Fprintln(os.Stdout, s)
}

func (c *Console) log(level Level, msg string) {
	if level < c.Level {
		return
	}

	prompt := ""
	formattedMsg := msg

	if c.Color {
		switch level {
		case WarnLevel:
			prompt = aurora.Yellow("⚠ ").String()
		case ErrorLevel, FatalLevel:
			prompt = aurora.Red("ⅹ ").String()
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for _, line := range strings.Split(formattedMsg, "\n") {
		if c.Color && level == DebugLevel {
			line = aurora.Faint(line).String()
		}
		line = prompt + line
		fmt.Fprintln(os.Stderr, line)
	}
}
