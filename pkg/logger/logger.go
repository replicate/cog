package logger

import (
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/util/console"
)

type Level int

const (
	LevelFatal Level = iota
	LevelError
	LevelWarn
	LevelStatus
	LevelInfo
	LevelDebug
)

// Logger is an interface for abstracting log output in a way that can be written to a log file, output to the console, or transported over the network.
type Logger interface {
	Info(line string)
	Debug(line string)
	Infof(line string, args ...interface{})
	Debugf(line string, args ...interface{})
	WriteStatus(status string, args ...interface{})
	WriteError(err error)
	WriteVersion(version *model.Version)
}

type ConsoleLogger struct {
	prefix string
}

func NewConsoleLogger() *ConsoleLogger {
	return new(ConsoleLogger)
}

func (l *ConsoleLogger) Info(line string) {
	console.Info(l.prefix + line)
}

func (l *ConsoleLogger) Debug(line string) {
	console.Debug(l.prefix + line)
}

func (l *ConsoleLogger) Infof(line string, args ...interface{}) {
	console.Infof(l.prefix+line, args...)
}

func (l *ConsoleLogger) Debugf(line string, args ...interface{}) {
	console.Debugf(l.prefix+line, args...)
}

func (l *ConsoleLogger) WriteStatus(status string, args ...interface{}) {
	console.Infof(l.prefix+status, args...)
}

func (l *ConsoleLogger) WriteError(err error) {
	console.Error(l.prefix + err.Error())
}

// TODO(bfirsh): remove
func (l *ConsoleLogger) WriteVersion(version *model.Version) {
	console.Infof(l.prefix+"%v", version)
}
