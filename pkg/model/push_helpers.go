package model

import (
	"context"
	"os"
	"strconv"

	v1 "github.com/google/go-containerregistry/pkg/v1"

	"github.com/replicate/cog/pkg/registry"
)

const (
	// DefaultPushConcurrency is the default number of concurrent uploads
	// for both image layers and weight artifacts.
	// This matches Docker's default concurrency for layer uploads, which is a reasonable baseline for OCI pushes as well.
	DefaultPushConcurrency = 5

	// envPushConcurrency is the environment variable that overrides DefaultPushConcurrency.
	envPushConcurrency = "COG_PUSH_CONCURRENCY"
)

// GetPushConcurrency returns the push concurrency, checking the COG_PUSH_CONCURRENCY
// environment variable first, then falling back to DefaultPushConcurrency.
func GetPushConcurrency() int {
	if v := os.Getenv(envPushConcurrency); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return DefaultPushConcurrency
}

// PushPhase represents a phase of the push process.
// The image pusher reports phase transitions so the CLI can display appropriate
// progress indicators (e.g., a status line during export, progress bars during push).
type PushPhase string

const (
	// PushPhaseExporting indicates the image is being exported from the Docker
	// daemon to a local tarball. This phase has no granular progress — the
	// caller typically shows an indeterminate status indicator.
	PushPhaseExporting PushPhase = "exporting"

	// PushPhasePushing indicates layers are being pushed to the registry.
	// During this phase, per-layer progress is reported via PushProgress callbacks.
	PushPhasePushing PushPhase = "pushing"
)

// PushProgress reports progress for a push operation.
//
// There are two kinds of updates:
//   - Phase transitions: Phase is set, byte fields are zero. Indicates the push
//     has moved to a new phase (e.g., exporting image, pushing layers).
//   - Byte progress: Phase is empty, Complete/Total track upload progress for
//     a specific layer or blob identified by LayerDigest.
//
// Used by both ImagePusher (container image layers) and WeightPusher (weight blobs).
type PushProgress struct {
	// Phase indicates a push phase transition. When set, this is a phase-only
	// update and the byte progress fields should be ignored.
	Phase PushPhase
	// LayerDigest identifies which layer this progress is for.
	// Empty for phase transitions and single-layer pushes (e.g., weight uploads).
	LayerDigest string
	// Complete is the number of bytes uploaded so far.
	Complete int64
	// Total is the total number of bytes to upload.
	Total int64
}

// writeLayerWithProgress pushes a layer via registry.WriteLayer, managing the
// progress channel lifecycle (create, drain, close) on behalf of the caller.
//
// onProgress is called for each v1.Update from the registry. If nil, no progress
// channel is created and no goroutine is spawned.
func writeLayerWithProgress(ctx context.Context, reg registry.Client, opts registry.WriteLayerOptions, onProgress func(v1.Update)) error {
	var progressCh chan v1.Update
	var progressDone chan struct{}

	if onProgress != nil {
		progressCh = make(chan v1.Update, 100)
		progressDone = make(chan struct{})
		go func() {
			defer close(progressDone)
			for update := range progressCh {
				onProgress(update)
			}
		}()
		opts.ProgressCh = progressCh
	}

	writeErr := reg.WriteLayer(ctx, opts)

	// Close the progress channel ourselves — WriteLayer sends to it but does not close it.
	if progressCh != nil {
		close(progressCh)
	}
	if progressDone != nil {
		<-progressDone
	}

	return writeErr
}
