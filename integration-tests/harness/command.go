package harness

import "github.com/rogpeppe/go-internal/testscript"

// Command defines the interface for testscript commands.
type Command interface {
	// Name returns the command name as used in txtar scripts.
	Name() string

	// Run executes the command.
	// neg is true if the command was prefixed with '!' (expecting failure).
	// args are the command arguments.
	Run(ts *testscript.TestScript, neg bool, args []string)
}

// CommandFunc adapts a function to the Command interface.
type CommandFunc struct {
	name string
	fn   func(ts *testscript.TestScript, neg bool, args []string)
}

func (c CommandFunc) Name() string { return c.name }
func (c CommandFunc) Run(ts *testscript.TestScript, neg bool, args []string) {
	c.fn(ts, neg, args)
}

// NewCommand creates a Command from a name and function.
func NewCommand(name string, fn func(ts *testscript.TestScript, neg bool, args []string)) Command {
	return CommandFunc{name: name, fn: fn}
}
