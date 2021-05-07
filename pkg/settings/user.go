package settings

import (
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path"
	"runtime"

	"github.com/replicate/cog/pkg/util/files"
)

type AuthInfo struct {
	Token    string `json:"token"`
	Username string `json:"username"`
}

type UserSettings struct {
	Auth map[string]AuthInfo `json:"auth"`
}

func SaveAuthToken(address string, username string, token string) error {
	var err error

	settingsPath, err := getUserSettingsPath()
	if err != nil {
		return err
	}

	var settings *UserSettings
	exists, err := files.Exists(settingsPath)
	if err != nil {
		return err
	}
	if exists {
		settings, err = LoadUserSettings()
		if err != nil {
			return err
		}

	} else {
		settings = &UserSettings{}
	}
	if settings.Auth == nil {
		settings.Auth = map[string]AuthInfo{}
	}
	settings.Auth[address] = AuthInfo{
		Token:    token,
		Username: username,
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(settingsPath, data, 0600)
}

func LoadUserSettings() (*UserSettings, error) {
	settingsPath, err := getUserSettingsPath()
	if err != nil {
		return nil, fmt.Errorf("Failed to determine settings path")
	}

	exists, err := files.Exists(settingsPath)
	if err != nil {
		return nil, err
	}
	if !exists {
		return new(UserSettings), nil
	}

	text, err := os.ReadFile(settingsPath)
	if err != nil {
		return nil, fmt.Errorf("Failed to load settings. Did you run cog login?")
	}

	settings := UserSettings{}
	err = json.Unmarshal(text, &settings)
	if err != nil {
		return nil, fmt.Errorf("%s is corrupted. Please re-run cog login", settingsPath)
	}

	return &settings, nil
}

func LoadAuthToken(address string) (string, error) {
	s, err := LoadUserSettings()
	if err != nil {
		return "", err
	}
	return s.Token(address), nil
}

func (s *UserSettings) Token(address string) string {
	authToken, ok := s.Auth[address]
	if !ok {
		return ""
	}
	return authToken.Token
}

func (s *UserSettings) Username(address string) (token string, err error) {
	authToken, ok := s.Auth[address]
	if !ok {
		return "", fmt.Errorf("You are not logged in! Run \"cog login\" to get started")
	}
	return authToken.Username, nil
}

func getUserSettingsPath() (string, error) {
	configDir, err := userConfigDir()
	if err != nil {
		return "", err
	}

	folder := path.Join(configDir, "cog")
	err = os.MkdirAll(folder, os.ModePerm)
	if err != nil {
		return "", err
	}
	settingsPath := path.Join(folder, "settings.json")

	return settingsPath, nil
}

func userConfigDir() (string, error) {
	switch runtime.GOOS {
	case "linux":
		return os.UserConfigDir()
	case "windows":
		return os.UserConfigDir()
	case "darwin":
		usr, err := user.Current()
		if err != nil {
			return os.UserConfigDir()
		}
		return path.Join(usr.HomeDir, ".config"), nil
	default:
		return os.UserConfigDir()
	}
}
