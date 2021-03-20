package settings

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/mitchellh/go-homedir"
	log "github.com/sirupsen/logrus"

	"github.com/replicate/cog/pkg/files"
)

// UserSettings represents global user settings that span multiple projects
type UserSettings struct {
	Remote string `json:"remote"`
}

// LoadUserSettings loads the global user settings from disk, returning default struct
// if no file exists
func LoadUserSettings() (*UserSettings, error) {
	settings := UserSettings{
		Remote: "http://localhost:8080",
	}

	settingsPath, err := userSettingsPath()
	if err != nil {
		return nil, err
	}

	exists, err := files.FileExists(settingsPath)
	if err != nil {
		return nil, err
	}
	if !exists {
		return &settings, nil
	}
	text, err := ioutil.ReadFile(settingsPath)
	if err != nil {
		log.Warnf("Failed to read %s: %s", settingsPath, err)
		return &settings, nil
	}

	err = json.Unmarshal(text, &settings)
	if err != nil {
		return nil, err
	}

	return &settings, nil
}

// Save saves global user settings to disk
func (s *UserSettings) Save() error {
	settingsPath, err := userSettingsPath()
	if err != nil {
		return err
	}

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

func UserSettingsDir() (string, error) {
	return homedir.Expand("~/.config/cog")
}

func userSettingsPath() (string, error) {
	dir, err := UserSettingsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "settings.json"), nil
}
