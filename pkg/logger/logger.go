package logger

import (
	"github.com/replicate/cog/pkg/console"

	"github.com/replicate/cog/pkg/model"
)

type Logger interface {
	WriteLogLine(line string, args ...interface{})
	WriteDebugLine(line string, args ...interface{})
	WriteStatus(status string, args ...interface{})
	WriteError(err error)
	WriteModel(mod *model.Model)
}

type ConsoleLogger struct {
}

func NewConsoleLogger() *ConsoleLogger {
	return new(ConsoleLogger)
}

func (l *ConsoleLogger) WriteLogLine(line string, args ...interface{}) {
	console.Info(line, args...)
}

func (l *ConsoleLogger) WriteDebugLine(line string, args ...interface{}) {
	console.Debug(line, args...)
}

func (l *ConsoleLogger) WriteStatus(status string, args ...interface{}) {
	console.Info(status, args...)
}

func (l *ConsoleLogger) WriteError(err error) {
	console.Error(err.Error())
}

func (l *ConsoleLogger) WriteModel(mod *model.Model) {
	console.Info("%v", mod)
}
