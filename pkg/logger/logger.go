package logger

import (
	"github.com/replicate/cog/pkg/util/console"

	"github.com/replicate/cog/pkg/model"
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

type Logger interface {
	Info(line string)
	Debug(line string)
	Infof(line string, args ...interface{})
	Debugf(line string, args ...interface{})
	WriteStatus(status string, args ...interface{})
	WriteError(err error)
	WriteModel(mod *model.Model)
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

func (l *ConsoleLogger) WriteModel(mod *model.Model) {
	console.Infof(l.prefix+"%v", mod)
}
