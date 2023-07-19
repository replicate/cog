package console

import (
	"os"

	"github.com/mattn/go-isatty"
)

// ConsoleInstance is the global instance of console, so we don't have to pass it around everywhere
var ConsoleInstance = &Console{
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
func Debug(msg string) {
	ConsoleInstance.Debug(msg)
}

// Info level message.
func Info(msg string) {
	ConsoleInstance.Info(msg)
}

// Warn level message.
func Warn(msg string) {
	ConsoleInstance.Warn(msg)
}

// Error level message.
func Error(msg string) {
	ConsoleInstance.Error(msg)
}

// Fatal level message.
func Fatal(msg string) {
	ConsoleInstance.Fatal(msg)
}

// Debug level message.
func Debugf(msg string, v ...interface{}) {
	ConsoleInstance.Debugf(msg, v...)
}

// Info level message.
func Infof(msg string, v ...interface{}) {
	ConsoleInstance.Infof(msg, v...)
}

// Warn level message.
func Warnf(msg string, v ...interface{}) {
	ConsoleInstance.Warnf(msg, v...)
}

// Error level message.
func Errorf(msg string, v ...interface{}) {
	ConsoleInstance.Errorf(msg, v...)
}

// Fatal level message.
func Fatalf(msg string, v ...interface{}) {
	ConsoleInstance.Fatalf(msg, v...)
}

// Output a line to stdout. Useful for printing primary output of a command, or the output of a subcommand.
func Output(s string) {
	ConsoleInstance.Output(s)
}

// IsTTY checks if a file is a TTY or not. E.g. IsTTY(os.Stdin)
func IsTTY(f *os.File) bool {
	return isatty.IsTerminal(f.Fd())
}
