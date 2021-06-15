package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/lib/pq"

	"github.com/replicate/cog/pkg/logger"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/util/console"
)

type versionDBValue struct {
	version *model.Version
}

func (v *versionDBValue) Value() (driver.Value, error) {
	return json.Marshal(v.version)
}

func (v *versionDBValue) Scan(value interface{}) error {
	b, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("Invalid database value for version")
	}
	version := new(model.Version)
	if err := json.Unmarshal(b, version); err != nil {
		return fmt.Errorf("Failed to version database value")
	}
	v.version = version
	return nil
}

type imageDBValue struct {
	image *model.Image
}

func (v *imageDBValue) Value() (driver.Value, error) {
	return json.Marshal(v.image)
}

func (v *imageDBValue) Scan(value interface{}) error {
	b, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("Invalid database value for image")
	}
	image := new(model.Image)
	if err := json.Unmarshal(b, image); err != nil {
		return fmt.Errorf("Failed to image database value")
	}
	v.image = image
	return nil
}

type PostgresDatabase struct {
	db *sql.DB
}

func NewPostgresDatabase(host string, port int, user string, password string, dbName string) (*PostgresDatabase, error) {
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		host,
		port,
		user,
		password,
		dbName)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("Failed to connect to Postgres: %w", err)
	}
	postgres := &PostgresDatabase{db: db}
	if err := retry(func() error {
		return postgres.ping()
	}, 20, 2*time.Second); err != nil {
		return nil, err
	}
	return postgres, nil
}

func (p *PostgresDatabase) InsertVersion(user string, name string, id string, version *model.Version) error {
	_, err := p.db.ExecContext(context.Background(), "INSERT INTO versions (username, model_name, id, data) VALUES($1, $2, $3, $4)", user, name, id, &versionDBValue{version})
	if err != nil {
		return fmt.Errorf("Failed to insert version into database: %w", err)
	}
	return nil
}

func (p *PostgresDatabase) UpdateVersion(user string, name string, id string, version *model.Version) error {
	_, err := p.db.ExecContext(context.Background(), "UPDATE versions SET data = $4 WHERE username = $1 AND model_name = $2 AND id = $3", user, name, id, &versionDBValue{version})
	if err != nil {
		return fmt.Errorf("Failed to insert version into database: %w", err)
	}
	return nil
}

func (p *PostgresDatabase) GetVersion(user string, name string, id string) (*model.Version, error) {
	value := new(versionDBValue)
	err := p.db.QueryRow("SELECT data FROM versions WHERE username = $1 AND model_name = $2 AND id = $3 LIMIT 1", user, name, id).Scan(value)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("Failed to read version from database: %w", err)
	}
	return value.version, nil
}

func (p *PostgresDatabase) ListVersions(user string, name string) ([]*model.Version, error) {
	rows, err := p.db.Query("SELECT data FROM versions WHERE username = $1 AND model_name = $2", user, name)
	if err != nil {
		return nil, fmt.Errorf("Failed to retrieve versions from database: %w", err)
	}
	versions := []*model.Version{}
	for rows.Next() {
		value := new(versionDBValue)
		if err := rows.Scan(value); err != nil {
			return nil, fmt.Errorf("Failed to parse version from database: %w", err)
		}
		versions = append(versions, value.version)
	}
	return versions, nil
}

func (p *PostgresDatabase) DeleteVersion(user string, name string, id string) error {
	result, err := p.db.Exec("DELETE FROM versions WHERE username = $1 AND model_name = $2 AND id = $3", user, name, id)
	if err != nil {
		return fmt.Errorf("Failed to delete version from database: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("Failed to determine rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("Failed to delete version: doesn't exist")
	}
	return nil
}

func (p *PostgresDatabase) InsertImage(user string, name string, id string, arch string, image *model.Image) error {
	_, err := p.db.ExecContext(context.Background(), "INSERT INTO images (username, model_name, version_id, arch, data) VALUES($1, $2, $3, $4, $5)", user, name, id, arch, &imageDBValue{image})
	if err != nil {
		return fmt.Errorf("Failed to insert image into database: %w", err)
	}
	return nil
}

func (p *PostgresDatabase) GetImage(user string, name string, id string, arch string) (*model.Image, error) {
	value := new(imageDBValue)
	err := p.db.QueryRow("SELECT data FROM images WHERE username = $1 AND model_name = $2 AND version_id = $3 AND arch = $4 LIMIT 1", user, name, id, arch).Scan(value)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("Failed to read image from database: %w", err)
	}
	return value.image, nil
}

func (p *PostgresDatabase) AddBuildLogLine(user string, name string, buildID string, line string, level logger.Level, timestampNano int64) error {
	_, err := p.db.ExecContext(context.Background(), "INSERT INTO build_log_lines (username, model_name, build_id, level, line, timestamp_nano) VALUES($1, $2, $3, $4, $5, $6)", user, name, buildID, level, line, timestampNano)
	if err != nil {
		return fmt.Errorf("Failed to insert build log line into database: %w", err)
	}
	return nil
}

// FinalizeBuildLog writes a final row to the build log with done=true
// to indicate that there will be no more output.
func (p *PostgresDatabase) FinalizeBuildLog(user string, name string, buildID string) error {
	timestampNano := time.Now().UTC().UnixNano()
	_, err := p.db.ExecContext(context.Background(), "INSERT INTO build_log_lines (username, model_name, build_id, done, level, line, timestamp_nano) VALUES($1, $2, $3, TRUE, $4, '', $5)", user, name, buildID, logger.LevelDebug, timestampNano)
	if err != nil {
		return fmt.Errorf("Failed to insert build log line into database: %w", err)
	}
	return nil
}

func (p *PostgresDatabase) GetBuildLogs(user string, name string, buildID string, follow bool) (chan *LogEntry, error) {
	logChan := make(chan *LogEntry)

	go func() {
		defer close(logChan)
		var latestTimestamp int64 = 0
		for {
			rows, err := p.db.Query("SELECT level, line, timestamp_nano, done FROM build_log_lines WHERE username = $1 AND model_name = $2 AND build_id = $3 AND timestamp_nano > $4 ORDER BY done, timestamp_nano", user, name, buildID, latestTimestamp)
			if err != nil {
				console.Errorf("Failed to read build logs from database: %v", err)
			}

			for rows.Next() {
				var level logger.Level
				var line string
				var timestampNano int64
				var done bool
				if err := rows.Scan(&level, &line, &timestampNano, &done); err != nil {
					console.Errorf("Failed to parse log line from database: %v", err)
				}

				logChan <- &LogEntry{
					Level:     level,
					Line:      line,
					Timestamp: timestampNano,
					Done:      done,
				}
				if done {
					return
				}

				latestTimestamp = timestampNano
			}
			if !follow {
				return
			}
		}
	}()

	return logChan, nil
}

func (p *PostgresDatabase) ListUserModels() ([]*UserModel, error) {
	rows, err := p.db.Query("SELECT DISTINCT username, model_name FROM versions")
	if err != nil {
		return nil, fmt.Errorf("Failed to fetch models from database: %w", err)
	}

	userModels := []*UserModel{}
	for rows.Next() {
		var username string
		var modelName string
		if err := rows.Scan(&username, &modelName); err != nil {
			console.Errorf("Failed to user and model from database: %w", err)
		}
		userModels = append(userModels, &UserModel{
			Username:  username,
			ModelName: modelName,
		})
	}
	return userModels, nil
}

func (p *PostgresDatabase) ping() error {
	pingContext, cancel := context.WithTimeout(context.Background(), cloudSQLConnectionTimeout)
	defer cancel()
	if err := p.db.PingContext(pingContext); err != nil {
		return fmt.Errorf("Failed to ping Postgres: %w", err)
	}
	return nil
}

func retry(fn func() error, retries int, sleep time.Duration) error {
	for i := 0; ; i++ {
		err := fn()
		if err == nil {
			return nil
		}
		if i == retries-1 {
			return fmt.Errorf("Failed after %d retries: %w", retries, err)
		}
		time.Sleep(sleep)
	}
}
