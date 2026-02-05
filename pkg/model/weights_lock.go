// pkg/model/weights_lock.go
package model

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// WeightsLockFilename is the default filename for the weights lock file.
const WeightsLockFilename = "weights.lock"

// WeightsLock represents a weights.lock file that pins weight file metadata.
// This is a placeholder format that will be replaced by the declarative weights implementation.
type WeightsLock struct {
	// Version is the lockfile format version.
	Version string `json:"version"`
	// Created is when the lockfile was generated.
	Created time.Time `json:"created"`
	// Files are the weight file entries.
	Files []WeightFile `json:"files"`
}

// ParseWeightsLock parses a weights.lock JSON document.
func ParseWeightsLock(data []byte) (*WeightsLock, error) {
	var lock WeightsLock
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, fmt.Errorf("parse weights.lock: %w", err)
	}
	return &lock, nil
}

// LoadWeightsLock loads a weights.lock file from disk.
func LoadWeightsLock(path string) (*WeightsLock, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read weights.lock: %w", err)
	}
	return ParseWeightsLock(data)
}

// Save writes the weights.lock to disk.
func (wl *WeightsLock) Save(path string) error {
	data, err := json.MarshalIndent(wl, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal weights.lock: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write weights.lock: %w", err)
	}
	return nil
}

// ToWeightsManifest converts the lockfile to a WeightsManifest.
func (wl *WeightsLock) ToWeightsManifest() *WeightsManifest {
	return &WeightsManifest{
		ArtifactType: MediaTypeWeightArtifact,
		Created:      wl.Created,
		Files:        wl.Files,
	}
}
