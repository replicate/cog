package model

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"

	"github.com/replicate/cog/pkg/model/weightsource"
)

// WeightsCacheDir is the project-relative directory where the builder
// writes packed tar layers. Pack output survives across build → push so
// repeated imports without source changes skip the tar work.
const WeightsCacheDir = ".cog/weights-cache"

// WeightBuilder is the weight factory: given a WeightSpec (source URI +
// target), it resolves the source via the weightsource.Source interface,
// packs the materialized directory into tar layers via packer.Pack,
// assembles the v1 OCI manifest, and returns a WeightArtifact carrying
// the layer descriptors and manifest digest.
//
// The builder is offline: it never talks to a registry. The manifest
// digest it writes into the artifact descriptor is a sha256 of the
// serialized manifest bytes.
type WeightBuilder struct {
	source   *Source
	lockPath string
}

// NewWeightBuilder creates a WeightBuilder.
// lockPath is where weights.lock is read/written as a build cache.
func NewWeightBuilder(source *Source, lockPath string) *WeightBuilder {
	return &WeightBuilder{source: source, lockPath: lockPath}
}

// Build packs the weight source directory into tar layers, assembles the
// v1 OCI manifest, updates the lockfile, and returns a WeightArtifact
// ready to push.
//
// The lockfile serves as a build cache: if an entry with the same name +
// layer set exists and every cached tar is still on disk at the expected
// size, Pack is skipped. Any miss triggers a full repack.
//
// The lockfile is rewritten only when the new entry differs from the
// existing one in either content or source metadata — a pure cache hit
// against the same source is a no-op on disk.
func (b *WeightBuilder) Build(ctx context.Context, spec ArtifactSpec) (Artifact, error) {
	ws, ok := spec.(*WeightSpec)
	if !ok {
		return nil, fmt.Errorf("weight builder: expected *WeightSpec, got %T", spec)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	projectDir := b.projectDir()

	src, err := weightsource.For(ws.Source, projectDir)
	if err != nil {
		return nil, err
	}
	normalizedURI, err := weightsource.NormalizeURI(ws.Source)
	if err != nil {
		return nil, fmt.Errorf("weight %q: %w", ws.Name(), err)
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

	var entry WeightLockEntry
	if hit {
		// Cache hit: skip the source walk entirely. The lockfile carries
		// the file index and the fingerprint; source-drift detection is a
		// separate concern (see cog-wej9).
		entry = *existing
	} else {
		inv, err := src.Inventory(ctx)
		if err != nil {
			return nil, fmt.Errorf("inventory weight %q: %w", ws.Name(), err)
		}

		if err := resetCacheDir(cacheDir); err != nil {
			return nil, err
		}
		pr, err := NewPacker(&PackOptions{TempDir: cacheDir}).Pack(ctx, src, inv)
		if err != nil {
			return nil, fmt.Errorf("pack weight %q: %w", ws.Name(), err)
		}
		// Rename each packer-produced tar to a content-addressed path so
		// subsequent cache lookups can find it without extra bookkeeping.
		for i, lr := range pr.Layers {
			target := layerCachePath(cacheDir, lr.Digest, lr.MediaType)
			if target == lr.TarPath {
				continue
			}
			if err := os.Rename(lr.TarPath, target); err != nil {
				cleanupPackedLayers(pr.Layers)
				return nil, fmt.Errorf("move layer %s to cache: %w", lr.Digest, err)
			}
			pr.Layers[i].TarPath = target
		}
		layers = pr.Layers

		// Build a preliminary entry so we can derive config blob and
		// manifest from lockfile fields. The manifest digest is filled
		// in below once we've assembled the manifest.
		entry = NewWeightLockEntry(
			ws.Name(), ws.Target,
			WeightLockSource{
				URI:         normalizedURI,
				Fingerprint: inv.Fingerprint,
				Include:     []string{},
				Exclude:     []string{},
				ImportedAt:  time.Now().UTC(),
			},
			pr.Files,
			pr.Layers,
		)
	}

	artifact, err := BuildWeightArtifact(&entry, layers)
	if err != nil {
		cleanupPackedLayers(layers)
		return nil, fmt.Errorf("weight %q: %w", ws.Name(), err)
	}

	if !lockEntriesEqual(existing, &entry) {
		// Preserve ImportedAt across pure-content cache hits: if only
		// ImportedAt would differ, the existing entry's content and
		// source already match, and we shouldn't churn the timestamp.
		// lockEntriesEqual ignores ImportedAt, so reaching here means
		// something material changed — record the new timestamp.
		lock.Upsert(entry)
		if err := lock.Save(b.lockPath); err != nil {
			cleanupPackedLayers(layers)
			return nil, fmt.Errorf("update lockfile: %w", err)
		}
	}

	return artifact, nil
}

// projectDir returns the builder's project directory, or "" if the
// builder was constructed without a Source. The builder falls back to
// the empty string rather than panicking so error messages from the
// Source interface bubble up in the normal way.
func (b *WeightBuilder) projectDir() string {
	if b.source == nil {
		return ""
	}
	return b.source.ProjectDir
}

// cacheDirFor returns the project-local cache directory for a weight
// name, creating it if necessary.
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

// resetCacheDir wipes the cache directory and recreates it. The combined
// RemoveAll+MkdirAll avoids leaking stale tars from a previous failed
// pack while being simpler than walking directory entries.
func resetCacheDir(dir string) error {
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("clear cache dir: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("recreate cache dir: %w", err)
	}
	return nil
}

// cachedLayers returns the layer results for a weight if every tar
// referenced by the lockfile entry is still on disk in cacheDir with the
// expected size. Size-only check is sufficient because the filename is
// content-addressed; a truncated file with the exact expected size would
// also have to have a matching digest, which is effectively impossible.
// Returns (nil, false) on any miss.
func cachedLayers(entry *WeightLockEntry, cacheDir string) ([]PackedLayer, bool) {
	if entry == nil || len(entry.Layers) == 0 {
		return nil, false
	}
	results := make([]PackedLayer, 0, len(entry.Layers))
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
		results = append(results, PackedLayer{
			TarPath:          tarPath,
			Digest:           hash,
			Size:             l.Size,
			UncompressedSize: l.SizeUncompressed,
			MediaType:        mt,
		})
	}
	return results, true
}

// loadLockfileOrEmpty loads the lockfile at path. A missing file is not
// an error — it yields a fresh empty lockfile.
func loadLockfileOrEmpty(path string) (*WeightsLock, error) {
	lock, err := LoadWeightsLock(path)
	if err == nil {
		return lock, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return &WeightsLock{Version: WeightsLockVersion}, nil
	}
	return nil, err
}

// layerCachePath returns the on-disk path for a cached tar layer. The
// filename is derived from the layer digest so different builds never
// collide and repacks can overwrite atomically. The media type decides
// the extension so tooling (and humans) can tell compressed from
// uncompressed layers at a glance.
func layerCachePath(cacheDir string, digest v1.Hash, mediaType types.MediaType) string {
	// `:` is not a safe path component on Windows or in tar archives.
	safe := digestToFilename(digest)
	ext := ".tar"
	if mediaType == MediaTypeOCILayerTarGzip {
		ext = ".tar.gz"
	}
	return filepath.Join(cacheDir, safe+ext)
}

// digestToFilename returns a filename-safe representation of a digest
// (colons replaced with dashes).
func digestToFilename(digest v1.Hash) string {
	return digest.Algorithm + "-" + digest.Hex
}
