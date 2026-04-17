package model

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

// WeightsCacheDir is the project-relative directory where the builder writes
// packed tar layers. Pack output survives across build → push so the common
// two-step flow (`cog weights build` then `cog weights push`) does not repack.
const WeightsCacheDir = ".cog/weights-cache"

// WeightBuilder is the weight factory: given a WeightSpec (directory +
// target), it walks the source, packs its contents into tar layers via
// packer.Pack, assembles the v1 OCI manifest, and returns a WeightArtifact
// carrying the layer descriptors and manifest digest.
//
// The builder is offline: it never talks to a registry. The manifest digest
// it writes into the artifact descriptor is a sha256 of the serialized
// manifest bytes in the standalone (no ReferenceDigest) shape.
type WeightBuilder struct {
	source   *Source
	lockPath string
}

// NewWeightBuilder creates a WeightBuilder.
// lockPath is where weights.lock is read/written as a build cache.
func NewWeightBuilder(source *Source, lockPath string) *WeightBuilder {
	return &WeightBuilder{source: source, lockPath: lockPath}
}

// Build packs the weight source directory into tar layers, assembles the v1
// OCI manifest, updates the lockfile, and returns a WeightArtifact ready to
// push.
//
// The lockfile serves as a build cache: if an entry with the same name +
// layer set exists and every cached tar is still on disk at the expected
// size, Pack is skipped. Any miss triggers a full repack.
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

	absSource, err := b.resolveSource(ws.Source)
	if err != nil {
		return nil, err
	}

	cacheDir, err := b.cacheDirFor(ws.Name())
	if err != nil {
		return nil, fmt.Errorf("prepare cache dir for %q: %w", ws.Name(), err)
	}

	lock, err := loadLockfileOrEmpty(b.lockPath)
	if err != nil {
		return nil, err
	}

	existing := lock.FindWeight(ws.Name())
	layers, hit := cachedLayers(existing, cacheDir)
	if !hit {
		// Rebuild from scratch. os.RemoveAll + MkdirAll is simpler than
		// walking entries, and always-fresh avoids stale tars leaking in
		// if a previous pack failed partway.
		if err := os.RemoveAll(cacheDir); err != nil {
			return nil, fmt.Errorf("clear cache dir: %w", err)
		}
		if err := os.MkdirAll(cacheDir, 0o755); err != nil {
			return nil, fmt.Errorf("recreate cache dir: %w", err)
		}

		packed, err := Pack(ctx, absSource, &PackOptions{TempDir: cacheDir})
		if err != nil {
			return nil, fmt.Errorf("pack weight %q: %w", ws.Name(), err)
		}
		// Rename each packer-produced tar to a content-addressed path so
		// subsequent cache lookups can find it without extra bookkeeping.
		// Pack uses os.CreateTemp, which picks random suffixes; without
		// this the cache would miss every time.
		for i, lr := range packed {
			target := layerCachePath(cacheDir, lr.Digest, lr.MediaType)
			if target == lr.TarPath {
				continue
			}
			if err := os.Rename(lr.TarPath, target); err != nil {
				cleanupLayerResults(packed)
				return nil, fmt.Errorf("move layer %s to cache: %w", lr.Digest, err)
			}
			packed[i].TarPath = target
		}
		layers = packed
	}

	// Assemble the manifest to record a descriptor. BuildWeightManifestV1 is
	// deterministic given identical layers + metadata, including Created.
	created := time.Now().UTC().Truncate(time.Second)
	img, err := BuildWeightManifestV1(layers, WeightManifestV1Metadata{
		Name:    ws.Name(),
		Target:  ws.Target,
		Created: created,
	})
	if err != nil {
		cleanupLayerResults(layers)
		return nil, fmt.Errorf("build weight manifest %q: %w", ws.Name(), err)
	}
	desc, err := descriptorFromImage(img)
	if err != nil {
		cleanupLayerResults(layers)
		return nil, fmt.Errorf("compute manifest descriptor for %q: %w", ws.Name(), err)
	}

	// Only rewrite the lockfile if the entry actually changed. A
	// cache-hit build with the same spec should be a no-op on disk —
	// otherwise every `cog weights push` would gratuitously bump the
	// Created timestamp and make the file look modified to git.
	newEntry := NewWeightLockEntry(ws.Name(), ws.Target, desc.Digest.String(), layers)
	if !lockEntriesEqual(existing, &newEntry) {
		lock.Upsert(newEntry)
		if err := lock.Save(b.lockPath); err != nil {
			cleanupLayerResults(layers)
			return nil, fmt.Errorf("update lockfile: %w", err)
		}
	}

	wa := NewWeightArtifact(ws.Name(), desc, ws.Target, layers)
	wa.Created = created
	return wa, nil
}

// resolveSource resolves the weight source path against the project
// directory and checks that it exists and is a directory.
func (b *WeightBuilder) resolveSource(source string) (string, error) {
	absPath := source
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(b.source.ProjectDir, source)
	}

	fi, err := os.Stat(absPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("weight source not found: %s", source)
		}
		return "", fmt.Errorf("stat weight source %s: %w", source, err)
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("weight source %s is not a directory (v1 weights are directories)", source)
	}
	return absPath, nil
}

// cacheDirFor returns the project-local cache directory for a weight name,
// creating it if necessary.
func (b *WeightBuilder) cacheDirFor(name string) (string, error) {
	if b.source == nil || b.source.ProjectDir == "" {
		return "", fmt.Errorf("project directory not set")
	}
	dir := filepath.Join(b.source.ProjectDir, WeightsCacheDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// cachedLayers returns the layer results for a weight if every tar
// referenced by the lockfile entry is still on disk in cacheDir with the
// expected size. Size-only check is sufficient because the filename is
// content-addressed; a truncated file with the exact expected size would
// also have to have a matching digest, which is effectively impossible.
// Returns (nil, false) on any miss.
func cachedLayers(entry *WeightLockEntry, cacheDir string) ([]LayerResult, bool) {
	if entry == nil || len(entry.Layers) == 0 {
		return nil, false
	}
	results := make([]LayerResult, 0, len(entry.Layers))
	for _, l := range entry.Layers {
		hash, err := v1.NewHash(l.Digest)
		if err != nil {
			return nil, false
		}
		mt := types.MediaType(l.MediaType)
		tarPath := layerCachePath(cacheDir, hash, mt)
		fi, err := os.Stat(tarPath)
		if err != nil || fi.Size() != l.Size {
			return nil, false
		}
		results = append(results, LayerResult{
			TarPath:     tarPath,
			Digest:      hash,
			Size:        l.Size,
			MediaType:   mt,
			Annotations: maps.Clone(l.Annotations),
		})
	}
	return results, true
}

// loadLockfileOrEmpty loads the lockfile at path. A missing file is not an
// error — it yields a fresh empty lockfile.
func loadLockfileOrEmpty(path string) (*WeightsLock, error) {
	lock, err := LoadWeightsLock(path)
	if err == nil {
		return lock, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return &WeightsLock{Version: WeightsLockVersion, Created: time.Now().UTC()}, nil
	}
	return nil, err
}

// layerCachePath returns the on-disk path for a cached tar layer. The
// filename is derived from the layer digest so different builds never
// collide and repacks can overwrite atomically. The media type decides the
// extension so tooling (and humans) can tell compressed from uncompressed
// layers at a glance.
func layerCachePath(cacheDir string, digest v1.Hash, mediaType types.MediaType) string {
	// `:` is not a safe path component on Windows or in tar archives.
	safe := strings.ReplaceAll(digest.String(), ":", "-")
	ext := ".tar"
	if mediaType == MediaTypeOCILayerTarGzip {
		ext = ".tar.gz"
	}
	return filepath.Join(cacheDir, safe+ext)
}
