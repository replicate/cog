package weights

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"

	v1 "github.com/google/go-containerregistry/pkg/v1"

	"github.com/replicate/cog/pkg/model"
)

// PullResult summarizes what happened for a single weight during Pull.
// Returned in the same order as the input names (or lockfile order
// when names is empty).
type PullResult struct {
	Name          string
	FullyCached   bool
	FilesFetched  int
	BytesFetched  int64
	LayersFetched int
}

// PullEvent is emitted during Pull to drive progress output. Delivered
// on the calling goroutine in order; handlers MUST NOT block.
//
// Fields are populated per Kind — see each kind's comment.
type PullEvent struct {
	// Kind identifies which fields below are populated.
	Kind PullEventKind
	// Weight is set on every event.
	Weight string

	// WeightStart: summary of what's about to happen for the weight.
	// ManifestRef is set only when MissingFiles > 0 (fully-cached
	// weights need no registry round trip).
	Target       string
	TotalFiles   int
	MissingFiles int
	ManifestRef  string

	// LayerStart / LayerDone / FileStored: layer context.
	// LayerSize is 0 when the backing layer does not expose a size
	// (in-memory test layers).
	LayerDigest string
	LayerSize   int64

	// FileStored: per-file detail for a file just written to the store.
	FilePath   string
	FileDigest string
	FileSize   int64

	// WeightDone: cumulative totals for the weight. FullyCached is
	// true when no registry I/O happened.
	BytesFetched  int64
	FilesFetched  int
	LayersFetched int
	FullyCached   bool
}

type PullEventKind int

const (
	// PullEventUnknown is the zero value so that a freshly-constructed
	// PullEvent{} is distinguishable from a legitimate event.
	PullEventUnknown PullEventKind = iota
	PullEventWeightStart
	PullEventLayerStart
	PullEventFileStored
	PullEventLayerDone
	PullEventWeightDone
)

// Pull populates the local store with every file referenced by the
// lockfile for the named weights. Empty names means "all weights".
//
// Behavior:
//   - Files already present locally are skipped (no registry I/O).
//   - A layer is fetched only if at least one of its files is missing.
//     The whole layer must be streamed to reach any one file, so we
//     store every expected file the layer contains — PutFile is
//     idempotent so pre-cached files drain through without rewrites.
//   - Registry is authoritative. v1 does not fall back to the source
//     URI; re-run `cog weights import` if the registry is missing a
//     layer.
//   - Every file path in the tar must be in the lockfile. Unexpected
//     paths error out.
//
// onEvent, if non-nil, is called synchronously with each PullEvent.
func (m *Manager) Pull(ctx context.Context, names []string, onEvent func(PullEvent)) ([]PullResult, error) {
	entries, err := m.selectEntries(names)
	if err != nil {
		return nil, err
	}

	emit := onEvent
	if emit == nil {
		emit = func(PullEvent) {}
	}

	results := make([]PullResult, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return results, err
		}
		r, err := m.pullEntry(ctx, entry, emit)
		results = append(results, r)
		if err != nil {
			return results, fmt.Errorf("pull %s: %w", entry.Name, err)
		}
	}
	return results, nil
}

func (m *Manager) pullEntry(ctx context.Context, entry *model.WeightLockEntry, emit func(PullEvent)) (PullResult, error) {
	result := PullResult{Name: entry.Name}

	missingByLayer := map[string][]model.WeightLockFile{}
	var missingCount int
	for _, f := range entry.Files {
		ok, err := m.store.Exists(ctx, f.Digest)
		if err != nil {
			return result, fmt.Errorf("check %s: %w", f.Digest, err)
		}
		if ok {
			continue
		}
		missingByLayer[f.Layer] = append(missingByLayer[f.Layer], f)
		missingCount++
	}

	manifestRef := ""
	if missingCount > 0 {
		manifestRef = m.repo + "@" + entry.Digest
	}
	emit(PullEvent{
		Kind:         PullEventWeightStart,
		Weight:       entry.Name,
		Target:       entry.Target,
		TotalFiles:   len(entry.Files),
		MissingFiles: missingCount,
		ManifestRef:  manifestRef,
	})

	if missingCount == 0 {
		result.FullyCached = true
		emit(PullEvent{Kind: PullEventWeightDone, Weight: entry.Name, FullyCached: true})
		return result, nil
	}

	img, err := m.registry.GetImage(ctx, manifestRef, nil)
	if err != nil {
		return result, fmt.Errorf("fetch weight manifest %s: %w", manifestRef, err)
	}

	fileByPath := make(map[string]model.WeightLockFile, len(entry.Files))
	for _, f := range entry.Files {
		fileByPath[f.Path] = f
	}

	for layerDigest, needed := range missingByLayer {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if err := m.pullLayer(ctx, entry.Name, img, layerDigest, needed, fileByPath, emit); err != nil {
			return result, err
		}
		result.LayersFetched++
		for _, f := range needed {
			result.FilesFetched++
			result.BytesFetched += f.Size
		}
	}

	emit(PullEvent{
		Kind:          PullEventWeightDone,
		Weight:        entry.Name,
		BytesFetched:  result.BytesFetched,
		FilesFetched:  result.FilesFetched,
		LayersFetched: result.LayersFetched,
	})
	return result, nil
}

// pullLayer streams a layer's tar blob and stores every regular file
// it contains, verifying the expected files for this layer appeared.
func (m *Manager) pullLayer(
	ctx context.Context,
	weightName string,
	img v1.Image,
	layerDigest string,
	needed []model.WeightLockFile,
	fileByPath map[string]model.WeightLockFile,
	emit func(PullEvent),
) error {
	hash, err := v1.NewHash(layerDigest)
	if err != nil {
		return fmt.Errorf("parse layer digest %q: %w", layerDigest, err)
	}
	layer, err := img.LayerByDigest(hash)
	if err != nil {
		return fmt.Errorf("find layer %s: %w", layerDigest, err)
	}
	layerSize, _ := layer.Size()
	emit(PullEvent{
		Kind:        PullEventLayerStart,
		Weight:      weightName,
		LayerDigest: layerDigest,
		LayerSize:   layerSize,
	})

	rc, err := layer.Uncompressed()
	if err != nil {
		return fmt.Errorf("open layer %s: %w", layerDigest, err)
	}
	defer rc.Close() //nolint:errcheck // best-effort close on read path

	tr := tar.NewReader(rc)
	written := map[string]bool{}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read layer %s: %w", layerDigest, err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}

		file, ok := fileByPath[hdr.Name]
		if !ok {
			return fmt.Errorf("layer %s: unexpected file %q not in lockfile", layerDigest, hdr.Name)
		}

		if err := m.store.PutFile(ctx, file.Digest, file.Size, tr); err != nil {
			return fmt.Errorf("store %s (%s): %w", file.Path, file.Digest, err)
		}
		written[file.Path] = true
		emit(PullEvent{
			Kind:        PullEventFileStored,
			Weight:      weightName,
			LayerDigest: layerDigest,
			FilePath:    file.Path,
			FileDigest:  file.Digest,
			FileSize:    file.Size,
		})
	}

	for _, f := range needed {
		if !written[f.Path] {
			return fmt.Errorf("layer %s: missing expected file %q", layerDigest, f.Path)
		}
	}

	emit(PullEvent{Kind: PullEventLayerDone, Weight: weightName, LayerDigest: layerDigest})
	return nil
}
