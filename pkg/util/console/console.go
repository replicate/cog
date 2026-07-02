// Package console provides a standard interface for user- and machine-interface with the console
package console

import (
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/logrusorgru/aurora"
	"github.com/mattn/go-isatty"
	"golang.org/x/term"
)

// ShouldUseColor returns true if color output should be enabled, based on
// environment detection. It checks (in order):
//   - NO_COLOR env var is set and non-empty → no color
//   - COG_NO_COLOR env var is set and non-empty → no color
//   - TERM=dumb → no color
//   - stderr is not a TTY → no color
//
// This follows the NO_COLOR standard (https://no-color.org/) and common CLI
// conventions. The --no-color flag is handled separately at the CLI layer.
func ShouldUseColor() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("COG_NO_COLOR") != "" {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	fd := os.Stderr.Fd()
	if !isatty.IsTerminal(fd) && !isatty.IsCygwinTerminal(fd) {
		return false
	}
	return true
}

// Style controls the icon/color used for a log line, independent of level.
type Style int

const (
	// StyleDefault uses the default icon for the log level.
	StyleDefault Style = iota
	// StyleSuccess uses a green ✓ icon.
	StyleSuccess
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

// Fatal level message, followed by exit
func (c *Console) Fatal(msg string) {
	c.log(FatalLevel, msg)
	os.Exit(1)
}

// Debug level message
func (c *Console) Debugf(msg string, v ...any) {
	c.log(DebugLevel, fmt.Sprintf(msg, v...))
}

// Info level message
func (c *Console) Infof(msg string, v ...any) {
	c.log(InfoLevel, fmt.Sprintf(msg, v...))
}

// Success level message
func (c *Console) Successf(msg string, v ...any) {
	c.logStyled(InfoLevel, StyleSuccess, fmt.Sprintf(msg, v...))
}

// Warn level message
func (c *Console) Warnf(msg string, v ...any) {
	c.log(WarnLevel, fmt.Sprintf(msg, v...))
}

// Error level message
func (c *Console) Errorf(msg string, v ...any) {
	c.log(ErrorLevel, fmt.Sprintf(msg, v...))
}

// Fatal level message, followed by exit
func (c *Console) Fatalf(msg string, v ...any) {
	c.log(FatalLevel, fmt.Sprintf(msg, v...))
	os.Exit(1)
}

// InfoUnformatted writes a message to stderr without any prefix. Useful for conversational
// or interactive output (e.g. login prompts) where the icon prefix would be noise.
// Displayed at info level. Long lines are wrapped to terminal width when stderr is a TTY.
func (c *Console) InfoUnformatted(msg string) {
	if InfoLevel < c.Level {
		return
	}

	termWidth := stderrTerminalWidth()

	c.mu.Lock()
	defer c.mu.Unlock()

	for line := range strings.SplitSeq(msg, "\n") {
		if termWidth > 0 {
			wrapped := wrapLine(line, termWidth)
			for _, wl := range wrapped {
				fmt.Fprintln(os.Stderr, wl)
			}
			continue
		}
		fmt.Fprintln(os.Stderr, line)
	}
}

// InfoUnformattedf writes a formatted message to stderr without any prefix.
func (c *Console) InfoUnformattedf(msg string, v ...any) {
	c.InfoUnformatted(fmt.Sprintf(msg, v...))
}

// Output a string to stdout. Useful for printing primary output of a command, or the output of a subcommand.
// A newline is added to the string.
func (c *Console) Output(s string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, _ = fmt.Fprintln(os.Stdout, s)
}

// Bold applies bold formatting to a string when color is enabled.
// Use this to highlight dynamic values (image names, paths, URLs) in log messages.
func (c *Console) Bold(s string) string {
	if c.Color {
		return aurora.Bold(s).String()
	}
	return s
}

func (c *Console) log(level Level, msg string) {
	c.logStyled(level, StyleDefault, msg)
}

func (c *Console) logStyled(level Level, style Style, msg string) {
	if level < c.Level {
		return
	}

	prompt := ""
	// promptWidth is the visual width of the prompt (excluding ANSI codes).
	promptWidth := 0

	if c.Color {
		switch style {
		case StyleSuccess:
			prompt = " " + aurora.Bold(aurora.Green("✔ ")).String()
			promptWidth = 4 // " ✔ "
		default:
			switch level {
			case DebugLevel, InfoLevel:
				prompt = " " + aurora.Faint("⚙  ").String()
				promptWidth = 4 // " ⚙  "
			case WarnLevel:
				prompt = " " + aurora.Bold(aurora.Yellow("⚠ ")).String()
				promptWidth = 4 // " ⚠ "
			case ErrorLevel, FatalLevel:
				prompt = " " + aurora.Bold(aurora.Red("✗ ")).String()
				promptWidth = 4 // " ✗ "
			}
		}
	}

	termWidth := stderrTerminalWidth()

	c.mu.Lock()
	defer c.mu.Unlock()

	for line := range strings.SplitSeq(msg, "\n") {
		if line == "" && (level == DebugLevel || level == InfoLevel) {
			fmt.Fprintln(os.Stderr)
			continue
		}
		if c.Color && level == DebugLevel {
			line = aurora.Faint(line).String()
		}

		// Wrap long lines to terminal width.
		if termWidth > 0 && promptWidth > 0 {
			maxWidth := termWidth - promptWidth
			if maxWidth > 0 {
				wrapped := wrapLine(line, maxWidth)
				for _, wl := range wrapped {
					fmt.Fprintln(os.Stderr, prompt+wl)
				}
				continue
			}
		}

		fmt.Fprintln(os.Stderr, prompt+line)
	}
}

// stderrTerminalWidth returns the terminal width of stderr, or 0 if stderr
// is not a terminal or the width cannot be determined.
func stderrTerminalWidth() int {
	fd := os.Stderr.Fd()
	if !isatty.IsTerminal(fd) && !isatty.IsCygwinTerminal(fd) {
		return 0
	}
	if fd > math.MaxInt {
		return 0
	}
	w, _, err := term.GetSize(int(fd)) //nolint:gosec // bounded above
	if err != nil || w <= 0 {
		return 0
	}
	return w
}

// wrapLine wraps a single line of text to the given width, breaking on word
// boundaries where possible. It operates on the visible text (which may contain
// ANSI escape codes — these are counted as zero-width for wrapping purposes).
func wrapLine(line string, maxWidth int) []string {
	if visibleWidth(line) <= maxWidth {
		return []string{line}
	}

	var lines []string
	for len(line) > 0 {
		if visibleWidth(line) <= maxWidth {
			lines = append(lines, line)
			break
		}

		// Find the byte position where we exceed maxWidth visible chars.
		cutByte := findCutPoint(line, maxWidth)

		// Try to break at a space before the cut point.
		breakAt := strings.LastIndex(line[:cutByte], " ")
		if breakAt <= 0 {
			// No good break point; hard-break at cutByte.
			breakAt = cutByte
		}

		lines = append(lines, line[:breakAt])
		line = strings.TrimLeft(line[breakAt:], " ")
	}
	return lines
}

// visibleWidth returns the number of visible characters in a string,
// ignoring ANSI escape sequences.
func visibleWidth(s string) int {
	width := 0
	inEscape := false
	for _, r := range s {
		if inEscape {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
			continue
		}
		if r == '\x1b' {
			inEscape = true
			continue
		}
		width += utf8.RuneLen(r) // approximate: 1 for ASCII, may differ for wide chars
		if r > 127 {
			width = width - utf8.RuneLen(r) + 1 // count non-ASCII runes as width 1
		}
	}
	return width
}

// findCutPoint returns the byte index in s where the visible width reaches maxWidth.
func findCutPoint(s string, maxWidth int) int {
	width := 0
	inEscape := false
	for i, r := range s {
		if inEscape {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
			continue
		}
		if r == '\x1b' {
			inEscape = true
			continue
		}
		width++
		if width >= maxWidth {
			return i + utf8.RuneLen(r)
		}
	}
	return len(s)
}
