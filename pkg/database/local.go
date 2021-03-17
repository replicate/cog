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

func (db *LocalFileDatabase) InsertModel(mod *model.Model) error {
	data, err := json.Marshal(mod)
	if err != nil {
		return fmt.Errorf("Failed to marshall model: %w", err)
	}
	path := filepath.Join(db.rootDir, mod.ID+".json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("Failed to write metadata file %s: %w", path, err)
	}
	return nil
}

func (db *LocalFileDatabase) ListModels() ([]*model.Model, error) {
	entries, err := os.ReadDir(db.rootDir)
	if err != nil {
		return nil, fmt.Errorf("Failed to scan %s: %w", db.rootDir, err)
	}
	models := []*model.Model{}
	for _, entry := range entries {
		filename := entry.Name()
		if strings.HasSuffix(filename, ".json") {
			path := filepath.Join(db.rootDir, filename)
			mod, err := db.readModel(path)
			if err != nil {
				return nil, err
			}
			models = append(models, mod)
		}
	}
	return models, nil
}

// GetModelByID returns a model or nil if the model doesn't exist
func (db *LocalFileDatabase) GetModelByID(id string) (*model.Model, error) {
	path := filepath.Join(db.rootDir, id+".json")
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
