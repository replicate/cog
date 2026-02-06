package model

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
)

// WeightBuilder builds WeightArtifact from WeightSpec.
// It hashes the source file, creates a WeightConfig, and manages a lockfile as build cache.
type WeightBuilder struct {
	source     *Source
	cogVersion string
	lockPath   string
}

// NewWeightBuilder creates a WeightBuilder.
// lockPath is where the weights.lock file is read/written as a build cache.
func NewWeightBuilder(source *Source, cogVersion, lockPath string) *WeightBuilder {
	return &WeightBuilder{
		source:     source,
		cogVersion: cogVersion,
		lockPath:   lockPath,
	}
}

// Build builds a WeightArtifact from a WeightSpec.
// It resolves the source file, computes its SHA256 digest, and creates the artifact
// with a versioned WeightConfig.
func (b *WeightBuilder) Build(ctx context.Context, spec ArtifactSpec) (Artifact, error) {
	ws, ok := spec.(*WeightSpec)
	if !ok {
		return nil, fmt.Errorf("weight builder: expected *WeightSpec, got %T", spec)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Resolve file path
	absPath := filepath.Join(b.source.ProjectDir, ws.Source)

	// Stat the file to check existence and size
	fi, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("weight source not found: %s", ws.Source)
		}
		return nil, fmt.Errorf("stat weight file %s: %w", ws.Source, err)
	}

	// Check lockfile cache: if we have a matching entry (name + size), skip hashing.
	// NOTE: This cache only checks name + file size. Same-size modifications (rare for
	// weight files) won't be detected. Delete the lockfile to force re-hashing.
	// TODO: Consider adding mtime to the cache key for stronger invalidation.
	var digestStr string
	var size int64
	if cached := b.findCachedEntry(ws.Name(), fi.Size()); cached != nil {
		digestStr = cached.Digest
		size = cached.Size
	} else {
		// Cache miss: hash the file
		digestStr, size, err = hashFile(absPath)
		if err != nil {
			return nil, fmt.Errorf("hash weight file %s: %w", ws.Source, err)
		}
	}

	// Parse as v1.Hash for the descriptor
	digest, err := v1.NewHash(digestStr)
	if err != nil {
		return nil, fmt.Errorf("parse digest: %w", err)
	}

	// Build the WeightConfig
	cfg := WeightConfig{
		SchemaVersion: "1.0",
		CogVersion:    b.cogVersion,
		Name:          ws.Name(),
		Target:        ws.Target,
		Created:       time.Now().UTC(),
	}

	// Build the descriptor
	desc := v1.Descriptor{
		Digest:    digest,
		Size:      size,
		MediaType: MediaTypeWeightLayer,
	}

	// Update lockfile
	if err := b.updateLockfile(ws, digestStr, size); err != nil {
		return nil, fmt.Errorf("update lockfile: %w", err)
	}

	return NewWeightArtifact(ws.Name(), desc, absPath, ws.Target, cfg), nil
}

// findCachedEntry checks the lockfile for an entry matching name and fileSize.
// Returns the cached WeightFile if found and size matches, nil otherwise.
func (b *WeightBuilder) findCachedEntry(name string, fileSize int64) *WeightFile {
	if _, err := os.Stat(b.lockPath); err != nil {
		return nil
	}
	lock, err := LoadWeightsLock(b.lockPath)
	if err != nil {
		return nil
	}
	for i, f := range lock.Files {
		if f.Name == name && f.Size == fileSize {
			return &lock.Files[i]
		}
	}
	return nil
}

// updateLockfile loads the existing lockfile (if any), adds or updates
// the entry for the given weight, and saves it back.
func (b *WeightBuilder) updateLockfile(ws *WeightSpec, digest string, size int64) error {
	// Load existing lockfile, or start fresh.
	// LoadWeightsLock wraps the underlying error, so we check the raw file first.
	lock := &WeightsLock{
		Version: "1.0",
		Created: time.Now().UTC(),
	}
	if _, err := os.Stat(b.lockPath); err == nil {
		existing, loadErr := LoadWeightsLock(b.lockPath)
		if loadErr != nil {
			return fmt.Errorf("load existing lockfile: %w", loadErr)
		}
		lock = existing
	}

	entry := WeightFile{
		Name:             ws.Name(),
		Dest:             ws.Target,
		Digest:           digest,
		DigestOriginal:   digest,
		Size:             size,
		SizeUncompressed: size,
		MediaType:        MediaTypeWeightLayer,
	}

	// Update existing entry or append
	updated := false
	for i, f := range lock.Files {
		if f.Name == ws.Name() {
			lock.Files[i] = entry
			updated = true
			break
		}
	}
	if !updated {
		lock.Files = append(lock.Files, entry)
	}

	return lock.Save(b.lockPath)
}
