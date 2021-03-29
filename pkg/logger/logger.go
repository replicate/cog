package logger

import (
	log "github.com/sirupsen/logrus"

	"github.com/replicate/cog/pkg/model"
)

type Logger interface {
	WriteLogLine(line string, args ...interface{})
	WriteDebugLine(line string, args ...interface{})
	WriteStatus(status string, args ...interface{})
	WriteError(err error)
	WriteModel(mod *model.Model)
}

type LogrusLogger struct {
}

func NewLogrusLogger() *LogrusLogger {
	return new(LogrusLogger)
}

func (l *LogrusLogger) WriteLogLine(line string, args ...interface{}) {
	log.Infof(line, args...)
}

func (l *LogrusLogger) WriteDebugLine(line string, args ...interface{}) {
	log.Debugf(line, args...)
}

func (l *LogrusLogger) WriteStatus(status string, args ...interface{}) {
	log.Infof(status, args...)
}

func (l *LogrusLogger) WriteError(err error) {
	log.Error(err.Error())
}

func (l *LogrusLogger) WriteModel(mod *model.Model) {
	log.Infof("%v", mod)
}
