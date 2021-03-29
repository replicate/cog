package console

import (
	"os"

	"github.com/mattn/go-isatty"
)

// ConsoleInstance is the global instance of console, so we don't have to pass it around everywhere
var ConsoleInstance *Console = &Console{
	Color:     true,
	Level:     InfoLevel,
	IsMachine: false,
}

// SetLevel sets log level
func SetLevel(level Level) {
	ConsoleInstance.Level = level
}

// SetColor sets whether to print colors
func SetColor(color bool) {
	ConsoleInstance.Color = color
}

// Debug level message.
func Debug(msg string, v ...interface{}) {
	ConsoleInstance.Debug(msg, v...)
}

// Info level message.
func Info(msg string, v ...interface{}) {
	ConsoleInstance.Info(msg, v...)
}

// Warn level message.
func Warn(msg string, v ...interface{}) {
	ConsoleInstance.Warn(msg, v...)
}

// Error level message.
func Error(msg string, v ...interface{}) {
	ConsoleInstance.Error(msg, v...)
}

// Fatal level message.
func Fatal(msg string, v ...interface{}) {
	ConsoleInstance.Fatal(msg, v...)
}

// Output a line to stdout. Useful for printing primary output of a command, or the output of a subcommand.
func Output(line string) {
	ConsoleInstance.Output(line)
}

// OutputErr a line to stderr. Useful for printing primary output of a command, or the output of a subcommand.
func OutputErr(line string) {
	ConsoleInstance.OutputErr(line)
}

// DebugOutput a line to stdout. Like Output, but only when level is DebugLevel.
func DebugOutput(line string) {
	ConsoleInstance.DebugOutput(line)
}

// IsTTY checks if a file is a TTY or not. E.g. IsTTY(os.Stdin)
func IsTTY(f *os.File) bool {
	return isatty.IsTerminal(f.Fd())
}
