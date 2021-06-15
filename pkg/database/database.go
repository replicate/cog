package database

import (
	"fmt"
	"os"

	"github.com/replicate/cog/pkg/logger"
	"github.com/replicate/cog/pkg/model"
)

type LogEntry struct {
	Level     logger.Level `json:"level"`
	Line      string       `json:"line"`
	Timestamp int64        `json:"timestamp"`
	Done      bool         `json:"done"`
}

type UserModel struct {
	Username  string
	ModelName string
}

type Database interface {
	InsertVersion(user string, name string, id string, version *model.Version) error
	UpdateVersion(user string, name string, id string, version *model.Version) error

	// GetVersion returns a model or nil if the model doesn't exist
	GetVersion(user string, name string, id string) (*model.Version, error)

	ListVersions(user string, name string) ([]*model.Version, error)
	DeleteVersion(user string, name string, id string) error
	InsertImage(user string, name string, id string, arch string, image *model.Image) error
	GetImage(user string, name string, id string, arch string) (*model.Image, error)
	AddBuildLogLine(user string, name string, buildID string, line string, level logger.Level, timestampNano int64) error
	FinalizeBuildLog(user string, name string, buildID string) error
	GetBuildLogs(user string, name string, buildID string, follow bool) (chan *LogEntry, error)
	ListUserModels() ([]*UserModel, error)
}

func NewDatabase(database string, host string, port int, user string, password string, name string, localDatabaseDir string) (Database, error) {
	if database == "postgres" || database == "googlecloudsql" {
		if host == "" {
			return nil, fmt.Errorf("Database host is missing")
		}
		if user == "" {
			return nil, fmt.Errorf("Database user is missing")
		}
		if password == "" {
			return nil, fmt.Errorf("Database password is missing")
		}
		if name == "" {
			return nil, fmt.Errorf("Database name is missing")
		}
	}

	switch database {
	case "filesystem":
		if localDatabaseDir == "" {
			return nil, fmt.Errorf("Local database directory is missing")
		}
		if err := os.MkdirAll(localDatabaseDir, 0755); err != nil {
			return nil, fmt.Errorf("Failed to create %s: %w", localDatabaseDir, err)
		}
		return NewLocalFileDatabase(localDatabaseDir)
	case "postgres":
		return NewPostgresDatabase(host, port, user, password, name)
	case "googlecloudsql":
		return NewGoogleCloudSQLDatabase(host, user, password, name)
	default:
		return nil, fmt.Errorf("Unknown database: %s. Valid options are 'filesystem', 'postgres', 'googlecloudsql'", database)
	}
}
