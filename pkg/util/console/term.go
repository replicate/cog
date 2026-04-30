package console

import (
	"os"

	"github.com/moby/term"
)

// IsTerminal returns true if we're in a terminal and a user is interacting with us
func IsTerminal() bool {
	return term.IsTerminal(os.Stdin.Fd())
}
