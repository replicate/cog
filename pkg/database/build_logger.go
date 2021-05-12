package database

import (
	"fmt"
	"time"

	"github.com/replicate/cog/pkg/logger"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/util/console"
)

type BuildLogger struct {
	user     string
	repoName string
	buildID  string
	db       Database
}

func NewBuildLogger(user, repoName, buildID string, db Database) *BuildLogger {
	return &BuildLogger{
		user:     user,
		repoName: repoName,
		buildID:  buildID,
		db:       db,
	}
}

func (l *BuildLogger) Info(line string) {
	l.log(line, logger.LevelInfo)
}

func (l *BuildLogger) Debug(line string) {
	l.log(line, logger.LevelDebug)
}

func (l *BuildLogger) Infof(line string, args ...interface{}) {
	l.log(fmt.Sprintf(line, args...), logger.LevelInfo)
}

func (l *BuildLogger) Debugf(line string, args ...interface{}) {
	l.log(fmt.Sprintf(line, args...), logger.LevelDebug)
}

func (l *BuildLogger) WriteStatus(status string, args ...interface{}) {
	l.log(fmt.Sprintf(status, args...), logger.LevelStatus)
}

func (l *BuildLogger) WriteError(err error) {
	l.log(err.Error(), logger.LevelError)
}

func (l *BuildLogger) WriteVersion(version *model.Version) {
	panic("Call to WriteVersion in BuildLogger")
}

func (l *BuildLogger) log(line string, level logger.Level) {
	timestamp := time.Now().UTC().UnixNano()
	if err := l.db.AddBuildLogLine(l.user, l.repoName, l.buildID, line, level, timestamp); err != nil {
		console.Warnf("Failed to write log line: %v", err)
	}
}
