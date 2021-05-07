package database

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hpcloud/tail"

	"github.com/replicate/cog/pkg/logger"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/pkg/util/files"
)

type LocalFileDatabase struct {
	rootDir string
}

func NewLocalFileDatabase(rootDir string) (*LocalFileDatabase, error) {
	exists, err := files.Exists(rootDir)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("Root directory %s doesn't exist", rootDir)
	}
	isDir, err := files.IsDir(rootDir)
	if err != nil {
		return nil, err
	}
	if !isDir {
		return nil, fmt.Errorf("Root path %s is not a directory", rootDir)
	}
	db := &LocalFileDatabase{
		rootDir: rootDir,
	}
	return db, nil
}

func (db *LocalFileDatabase) InsertModel(user string, name string, id string, mod *model.Model) error {
	path := db.modelPath(user, name, id)
	exists, err := files.Exists(path)
	if err != nil {
		return err
	}
	if !exists {
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("Failed to create directory %s: %w", dir, err)
		}
	}
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("Failed to write metadata file %s: %w", path, err)
	}
	if json.NewEncoder(file).Encode(mod); err != nil {
		return fmt.Errorf("Failed to marshall model: %w", err)
	}
	return nil
}

// GetModel returns a model or nil if the model doesn't exist
func (db *LocalFileDatabase) GetModel(user string, name string, id string) (*model.Model, error) {
	path := db.modelPath(user, name, id)
	exists, err := files.Exists(path)
	if err != nil {
		return nil, fmt.Errorf("Failed to determine if %s exists: %w", path, err)
	}
	if !exists {
		return nil, nil
	}
	mod, err := db.readModel(path)
	if err != nil {
		return nil, err
	}
	return mod, nil
}

func (db *LocalFileDatabase) DeleteModel(user string, name string, id string) error {
	path := db.modelPath(user, name, id)
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("Failed to delete %s: %w", path, err)
	}
	return nil
}

func (db *LocalFileDatabase) ListModels(user string, name string) ([]*model.Model, error) {
	repoDir := db.repoDir(user, name)
	entries, err := os.ReadDir(repoDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*model.Model{}, nil
		}
		return nil, fmt.Errorf("Failed to scan %s: %w", db.rootDir, err)
	}
	models := []*model.Model{}
	for _, entry := range entries {
		filename := entry.Name()
		if strings.HasSuffix(filename, ".json") {
			path := filepath.Join(repoDir, filename)
			mod, err := db.readModel(path)
			if err != nil {
				return nil, err
			}
			models = append(models, mod)
		}
	}
	return models, nil
}

func (db *LocalFileDatabase) InsertImage(user string, name string, id string, arch string, image *model.Image) error {
	path := db.imagePath(user, name, id, arch)
	exists, err := files.Exists(path)
	if err != nil {
		return err
	}
	if !exists {
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("Failed to create directory %s: %w", dir, err)
		}
	}
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("Failed to write metadata file %s: %w", path, err)
	}
	if err := json.NewEncoder(file).Encode(image); err != nil {
		return fmt.Errorf("Failed to marshall model: %w", err)
	}
	return nil
}

// GetImage returns an image or nil if it doesn't exist
func (db *LocalFileDatabase) GetImage(user, name, id, arch string) (*model.Image, error) {
	path := db.imagePath(user, name, id, arch)
	exists, err := files.Exists(path)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}
	image, err := db.readImage(path)
	if err != nil {
		return nil, err
	}
	return image, nil
}

func (db *LocalFileDatabase) AddBuildLogLine(user, name, buildID, line string, level logger.Level, timestampNano int64) error {
	entry := &LogEntry{
		Level:     level,
		Line:      line,
		Timestamp: timestampNano,
	}
	return db.addBuildLogEntry(user, name, buildID, entry)
}

func (db *LocalFileDatabase) FinalizeBuildLog(user, name, buildID string) error {
	entry := &LogEntry{
		Done:      true,
		Timestamp: time.Now().UTC().UnixNano(),
	}
	return db.addBuildLogEntry(user, name, buildID, entry)
}

func (db *LocalFileDatabase) addBuildLogEntry(user, name, buildID string, entry *LogEntry) error {
	path := db.logPath(user, name, buildID)
	exists, err := files.Exists(path)
	if err != nil {
		return err
	}
	if !exists {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := json.NewEncoder(f).Encode(&entry); err != nil {
		return err
	}
	return nil
}

func (db *LocalFileDatabase) GetBuildLogs(user, name, buildID string, follow bool) (chan *LogEntry, error) {
	logChan := make(chan *LogEntry)
	path := db.logPath(user, name, buildID)
	exists, err := files.Exists(path)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("Build does not exist: %s", buildID)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("Failed to open build log: %w", err)
	}
	go func() {
		defer f.Close()
		defer close(logChan)
		if follow {
			t, err := tail.TailFile(path, tail.Config{Follow: true})
			if err != nil {
				console.Errorf("Failed to tail file: %v", err)
				return
			}
			for line := range t.Lines {
				entry := new(LogEntry)
				if err := json.Unmarshal([]byte(line.Text), entry); err != nil {
					console.Warnf("Failed to decode log entry: %v", err)
				}
				if entry.Done {
					return
				}
				logChan <- entry
			}
		} else {
			scanner := bufio.NewScanner(f)
			for scanner.Scan() {
				entry := new(LogEntry)
				if err := json.Unmarshal(scanner.Bytes(), entry); err != nil {
					console.Warnf("Failed to decode log entry: %v", err)
				}
				if entry.Done {
					return
				}
				logChan <- entry
			}
		}
	}()
	return logChan, nil
}

func (db *LocalFileDatabase) readModel(path string) (*model.Model, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("Failed to open %s: %w", path, err)
	}
	mod := new(model.Model)
	if err := json.NewDecoder(file).Decode(mod); err != nil {
		return nil, fmt.Errorf("Failed to parse %s: %w", path, err)
	}
	return mod, nil
}

func (db *LocalFileDatabase) readImage(path string) (*model.Image, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("Failed to read %s: %w", path, err)
	}
	image := new(model.Image)
	if err := json.NewDecoder(file).Decode(image); err != nil {
		return nil, fmt.Errorf("Failed to parse %s: %w", path, err)
	}
	return image, nil
}

func (db *LocalFileDatabase) modelPath(user string, name string, id string) string {
	// TODO(andreas): make this user/name/versions/id.json
	return filepath.Join(db.repoDir(user, name), id+".json")
}

func (db *LocalFileDatabase) logPath(user string, name string, buildID string) string {
	return filepath.Join(db.repoDir(user, name), "builds", buildID+".txt")
}

func (db *LocalFileDatabase) imagePath(user string, name string, id string, arch string) string {
	return filepath.Join(db.repoDir(user, name), "versions", id, "images", arch+".json")
}

func (db *LocalFileDatabase) repoDir(user string, name string) string {
	return filepath.Join(db.rootDir, user, name)
}
