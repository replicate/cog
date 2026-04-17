package model

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// WeightsLockFilename is the default filename for the weights lock file.
const WeightsLockFilename = "weights.lock"

// WeightsLockVersion is the current lockfile schema version.
//
// The lockfile schema mirrors the on-image /.cog/weights.json (spec §3.6):
// one entry per weight with the assembled manifest digest and the layer
// descriptors that make it up. Per-file entries from the pre-release v0
// format are intentionally unsupported — there is no migration path for a
// format that was never released.
const WeightsLockVersion = "v1"

// WeightsLock is the parsed representation of a weights.lock file.
type WeightsLock struct {
	// Version is the lockfile format version. Always WeightsLockVersion.
	Version string `json:"version"`
	// Created is when the lockfile was last written.
	Created time.Time `json:"created"`
	// Weights is one entry per declared weight.
	Weights []WeightLockEntry `json:"weights"`
}

// ParseWeightsLock parses a weights.lock JSON document and rejects anything
// that is not a v1 lockfile.
func ParseWeightsLock(data []byte) (*WeightsLock, error) {
	var lock WeightsLock
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, fmt.Errorf("parse weights.lock: %w", err)
	}
	if lock.Version != WeightsLockVersion {
		return nil, fmt.Errorf("unsupported weights.lock version %q (want %q)",
			lock.Version, WeightsLockVersion)
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

// Save writes the weights.lock to disk in canonical JSON form.
func (wl *WeightsLock) Save(path string) error {
	if wl.Version == "" {
		wl.Version = WeightsLockVersion
	}
	data, err := json.MarshalIndent(wl, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal weights.lock: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil { //nolint:gosec // lockfile is checked into the repo
		return fmt.Errorf("write weights.lock: %w", err)
	}
	return nil
}

// FindWeight returns the lockfile entry with the given name, or nil if
// no such entry exists.
func (wl *WeightsLock) FindWeight(name string) *WeightLockEntry {
	for i := range wl.Weights {
		if wl.Weights[i].Name == name {
			return &wl.Weights[i]
		}
	}
	return nil
}

// Upsert inserts or replaces the entry with the matching Name.
// It leaves all other entries untouched. Created is set to now().
func (wl *WeightsLock) Upsert(entry WeightLockEntry) {
	wl.Created = time.Now().UTC()
	for i := range wl.Weights {
		if wl.Weights[i].Name == entry.Name {
			wl.Weights[i] = entry
			return
		}
	}
	wl.Weights = append(wl.Weights, entry)
}

// lockEntriesEqual reports whether two entries describe the same weight:
// same manifest digest, same target, same layer set (by digest). Layer
// annotations are treated as metadata and not compared.
func lockEntriesEqual(a, b *WeightLockEntry) bool {
	if a == nil || b == nil {
		return false
	}
	if a.Name != b.Name || a.Target != b.Target || a.Digest != b.Digest {
		return false
	}
	if len(a.Layers) != len(b.Layers) {
		return false
	}
	for i := range a.Layers {
		if a.Layers[i].Digest != b.Layers[i].Digest ||
			a.Layers[i].Size != b.Layers[i].Size ||
			a.Layers[i].MediaType != b.Layers[i].MediaType {
			return false
		}
	}
	return true
}
