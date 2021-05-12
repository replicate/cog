package database

import (
	"github.com/replicate/cog/pkg/logger"
	"github.com/replicate/cog/pkg/model"
)

type LogEntry struct {
	Level     logger.Level `json:"level"`
	Line      string       `json:"line"`
	Timestamp int64        `json:"timestamp"`
	Done      bool         `json:"done"`
}

type Database interface {
	InsertModel(user string, name string, id string, mod *model.Model) error
	GetModel(user string, name string, id string) (*model.Model, error)
	ListModels(user string, name string) ([]*model.Model, error)
	DeleteModel(user string, name string, id string) error
	InsertImage(user string, name string, id string, arch string, image *model.Image) error
	GetImage(user string, name string, id string, arch string) (*model.Image, error)
	AddBuildLogLine(user string, name string, buildID string, line string, level logger.Level, timestampNano int64) error
	FinalizeBuildLog(user string, name string, buildID string) error
	GetBuildLogs(user string, name string, buildID string, follow bool) (chan *LogEntry, error)
}
