package terminal

import (
	"context"
	"os"
	"strings"

	"github.com/containerd/console"
	"github.com/mattn/go-isatty"
	"golang.org/x/term"
)

// Returns a UI which will write to the current processes
// stdout/stderr.
func ConsoleUI(ctx context.Context) UI {
	// We do all of these checks because some sneaky environments fool
	// one or the other and we really only want the glint-based UI in
	// truly interactive environments.
	interactive := (isatty.IsTerminal(os.Stdout.Fd()) &&
		term.IsTerminal(int(os.Stdout.Fd())) &&
		strings.ToLower(os.Getenv("TERM")) != "dumb")
	if interactive {
		interactive = false
		if c, err := console.ConsoleFromFile(os.Stdout); err == nil {
			if sz, err := c.Size(); err == nil {
				interactive = sz.Height > 0 && sz.Width > 0
			}
		}
	}

	if interactive {
		return GlintUI(ctx)
	} else {
		return NonInteractiveUI(ctx)
	}
}
