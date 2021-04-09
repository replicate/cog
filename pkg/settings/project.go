package settings

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/replicate/cog/pkg/console"

	"github.com/replicate/cog/pkg/files"
	"github.com/replicate/cog/pkg/model"
)

type ProjectSettings struct {
	Repo        *model.Repo `json:"repo"`
	projectRoot string      `json:"-"`
}

func LoadProjectSettings(projectRoot string) (*ProjectSettings, error) {
	settings := &ProjectSettings{
		projectRoot: projectRoot,
	}

	settingsPath := projectSettingsPath(projectRoot)
	exists, err := files.Exists(settingsPath)
	if err != nil {
		return nil, err
	}
	if !exists {
		return settings, nil
	}
	text, err := ioutil.ReadFile(settingsPath)
	if err != nil {
		console.Warnf("Failed to read %s: %s", settingsPath, err)
		return settings, nil
	}

	err = json.Unmarshal(text, settings)
	if err != nil {
		return nil, err
	}

	return settings, nil
}

func (s *ProjectSettings) Save() error {
	settingsPath := projectSettingsPath(s.projectRoot)
	bytes, err := json.MarshalIndent(s, "", " ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(settingsPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	err = ioutil.WriteFile(settingsPath, bytes, 0600)
	if err != nil {
		return err
	}
	return nil
}

func ProjectSettingsDir(projectRoot string) string {
	return filepath.Join(projectRoot, ".cog")
}

func projectSettingsPath(projectRoot string) string {
	return filepath.Join(ProjectSettingsDir(projectRoot), "settings.json")
}
