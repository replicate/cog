package update

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/mitchellh/go-homedir"

	"github.com/replicate/cog/pkg/util/console"
	"github.com/replicate/cog/pkg/util/files"
)

type state struct {
	Message     string    `json:"message"`
	LastChecked time.Time `json:"lastChecked"`
	Version     string    `json:"version"`
}

// loadState loads the update check state from disk, returning defaults if it does not exist
func loadState() (*state, error) {
	state := state{}

	p, err := statePath()
	if err != nil {
		return nil, err
	}

	exists, err := files.Exists(p)
	if err != nil {
		return nil, err
	}
	if !exists {
		return &state, nil
	}
	text, err := os.ReadFile(p)
	if err != nil {
		console.Debugf("Failed to read %s: %s", p, err)
		return &state, nil
	}

	err = json.Unmarshal(text, &state)
	if err != nil {
		return nil, err
	}

	return &state, nil
}

// writeState saves analytics state to disk
func writeState(s *state) error {
	statePath, err := statePath()
	if err != nil {
		return err
	}

	bytes, err := json.MarshalIndent(s, "", " ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(statePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	err = os.WriteFile(statePath, bytes, 0o600)
	if err != nil {
		return err
	}
	return nil
}

func userDir() (string, error) {
	return homedir.Expand("~/.config/cog")
}

func statePath() (string, error) {
	dir, err := userDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "update-state.json"), nil
}
