package console

import (
	"os"

	"github.com/moby/term"
)

// IsTerminal returns true if we're in a terminal and a user is interacting with us
func IsTerminal() bool {
	return term.IsTerminal(os.Stdin.Fd())
}

// GetWidth returns the width of the terminal (from stderr -- stdout might be piped)
//
// Returns 0 if we're not in a terminal
func GetWidth() (uint16, error) {
	fd := os.Stderr.Fd()
	if term.IsTerminal(fd) {
		ws, err := term.GetWinsize(fd)
		if err != nil {
			return 0, err
		}
		return ws.Width, nil
	}
	return 0, nil
}
