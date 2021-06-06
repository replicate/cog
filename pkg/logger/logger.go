package logger

import (
	"fmt"

	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/pkg/util/terminal"
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

type TerminalLogger struct {
	ui        terminal.UI
	stepGroup terminal.StepGroup
	step      terminal.Step
}

func NewTerminalLogger(ui terminal.UI) *TerminalLogger {
	l := &TerminalLogger{ui: ui}
	l.stepGroup = ui.StepGroup()
	return l
}

func (l *TerminalLogger) Info(line string) {
	l.Infof(line)
}

func (l *TerminalLogger) Debug(line string) {
	l.Debugf(line)
}

func (l *TerminalLogger) Infof(line string, args ...interface{}) {
	if l.step != nil {
		l.step.Done()
	}
	l.step = l.stepGroup.Add(fmt.Sprintf(line, args...))
}

func (l *TerminalLogger) Debugf(line string, args ...interface{}) {
	if l.step != nil {
		fmt.Fprintf(l.step.TermOutput(), line+"\n", args...)
	}
}

func (l *TerminalLogger) WriteStatus(status string, args ...interface{}) {
	l.Infof("status: "+status, args...)
}

func (l *TerminalLogger) WriteError(err error) {
	l.step.Abort()
}

func (l *TerminalLogger) WriteVersion(version *model.Version) {
	l.Infof("version: %v", version)
}

func (l *TerminalLogger) Done() {
	l.step.Done()
}
