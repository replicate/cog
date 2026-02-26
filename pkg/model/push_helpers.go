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

// PushProgress reports progress for a layer or blob upload.
// Used by both ImagePusher (container image layers) and WeightPusher (weight blobs).
type PushProgress struct {
	// LayerDigest identifies which layer this progress is for.
	// Empty for single-layer pushes (e.g., weight uploads).
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
