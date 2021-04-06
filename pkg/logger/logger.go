package logger

import (
	"github.com/replicate/cog/pkg/console"

	"github.com/replicate/cog/pkg/model"
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
}

func NewConsoleLogger() *ConsoleLogger {
	return new(ConsoleLogger)
}

func (l *ConsoleLogger) Info(line string) {
	console.Info(line)
}

func (l *ConsoleLogger) Debug(line string) {
	console.Debug(line)
}

func (l *ConsoleLogger) Infof(line string, args ...interface{}) {
	console.Infof(line, args...)
}

func (l *ConsoleLogger) Debugf(line string, args ...interface{}) {
	console.Debugf(line, args...)
}

func (l *ConsoleLogger) WriteStatus(status string, args ...interface{}) {
	console.Infof(status, args...)
}

func (l *ConsoleLogger) WriteError(err error) {
	console.Error(err.Error())
}

func (l *ConsoleLogger) WriteModel(mod *model.Model) {
	console.Infof("%v", mod)
}
