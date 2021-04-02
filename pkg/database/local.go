package database

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"strings"

	"github.com/replicate/cog/pkg/files"
	"github.com/replicate/cog/pkg/model"
)

type LocalFileDatabase struct {
	rootDir string
}

func NewLocalFileDatabase(rootDir string) (*LocalFileDatabase, error) {
	exists, err := files.FileExists(rootDir)
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
	data, err := json.Marshal(mod)
	if err != nil {
		return fmt.Errorf("Failed to marshall model: %w", err)
	}
	path := db.modelPath(user, name, id)
	dir := filepath.Dir(path)
	exists, err := files.FileExists(path)
	if err != nil {
		return err
	}
	if !exists {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("Failed to create directory %s: %w", dir, err)
		}
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("Failed to write metadata file %s: %w", path, err)
	}
	return nil
}

// GetModel returns a model or nil if the model doesn't exist
func (db *LocalFileDatabase) GetModel(user string, name string, id string) (*model.Model, error) {
	path := db.modelPath(user, name, id)
	exists, err := files.FileExists(path)
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

func (db *LocalFileDatabase) readModel(path string) (*model.Model, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("Failed to read %s: %w", path, err)
	}
	mod := new(model.Model)
	if err := json.Unmarshal(contents, mod); err != nil {
		return nil, fmt.Errorf("Failed to parse %s: %w", path, err)
	}
	return mod, nil
}

func (db *LocalFileDatabase) modelPath(user string, name string, id string) string {
	return filepath.Join(db.repoDir(user, name), id+".json")
}

func (db *LocalFileDatabase) repoDir(user string, name string) string {
	return filepath.Join(db.rootDir, user, name)
}
