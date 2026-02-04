// pkg/model/weights_pusher.go
package model

import (
	"context"
	"fmt"
	"sync"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"

	"github.com/replicate/cog/pkg/registry"
)

// WeightsPushOptions configures the weights push operation.
type WeightsPushOptions struct {
	// Repo is the registry repository to push to (e.g., "registry.example.com/user/model").
	Repo string
	// Lock is the weights lock file containing file metadata.
	Lock *WeightsLock
	// FilePaths maps weight names to their local file paths.
	FilePaths map[string]string
	// ProgressFn is an optional callback for reporting progress of each file upload.
	ProgressFn func(WeightsPushProgress)
	// RetryFn is an optional callback for reporting retry attempts.
	// Return false from this callback to abort the retry.
	RetryFn func(WeightsRetryEvent) bool
	// RetryBackoff configures retry behavior. If nil, default backoff is used.
	RetryBackoff *remote.Backoff
}

// WeightsPushResult contains the results of pushing weights.
type WeightsPushResult struct {
	// Files contains the result for each file pushed.
	Files []WeightPushFileResult
	// TotalSize is the sum of successfully pushed file sizes.
	TotalSize int64
}

// WeightPushFileResult contains the result of pushing a single weight file.
type WeightPushFileResult struct {
	// Name is the weight file identifier.
	Name string
	// Digest is the layer digest after pushing.
	Digest string
	// Size is the layer size in bytes.
	Size int64
	// Err is any error that occurred during push.
	Err error
}

// WeightsPushProgress reports progress for a weight file upload.
type WeightsPushProgress struct {
	// Name identifies which file this progress is for.
	Name string
	// Complete is the number of bytes uploaded so far.
	Complete int64
	// Total is the total number of bytes to upload.
	Total int64
	// Done indicates the upload has finished (check Err for success/failure).
	Done bool
	// Err is any error that occurred (only set when Done is true).
	Err error
}

// WeightsRetryEvent reports a retry attempt for a weight file upload.
type WeightsRetryEvent struct {
	// Name identifies which file is being retried.
	Name string
	// Attempt is the current retry attempt number (1-indexed).
	Attempt int
	// MaxAttempts is the maximum number of retry attempts.
	MaxAttempts int
	// Err is the error that caused the retry.
	Err error
	// NextRetryIn is the duration until the next retry attempt.
	NextRetryIn time.Duration
}

// WeightsPusher handles pushing weight files to a registry.
type WeightsPusher struct {
	registry registry.Client
}

// NewWeightsPusher creates a new WeightsPusher.
func NewWeightsPusher(registry registry.Client) *WeightsPusher {
	return &WeightsPusher{registry: registry}
}

// Push pushes weight files to the registry concurrently.
func (p *WeightsPusher) Push(ctx context.Context, opts WeightsPushOptions) (*WeightsPushResult, error) {
	// Validate inputs
	if opts.Lock == nil {
		return nil, fmt.Errorf("weights lock is required")
	}
	if len(opts.Lock.Files) == 0 {
		return nil, fmt.Errorf("weights lock contains no files")
	}
	if opts.Repo == "" {
		return nil, fmt.Errorf("repository is required")
	}

	// Verify all files have paths
	for _, wf := range opts.Lock.Files {
		if _, ok := opts.FilePaths[wf.Name]; !ok {
			return nil, fmt.Errorf("file path not found for weight %q", wf.Name)
		}
	}

	// Helper to send progress if callback is set
	sendProgress := func(prog WeightsPushProgress) {
		if opts.ProgressFn != nil {
			opts.ProgressFn(prog)
		}
	}

	type pushResult struct {
		name   string
		digest string
		size   int64
		err    error
	}

	results := make(chan pushResult, len(opts.Lock.Files))
	var wg sync.WaitGroup

	const maxConcurrency = 4 // TODO: make this configurable or use a worker pool
	sem := make(chan struct{}, maxConcurrency)

	// Push each file in a separate goroutine with concurrency limit
	for _, wf := range opts.Lock.Files {
		wg.Add(1)
		go func(wf WeightFile) {
			sem <- struct{}{} // acquire
			defer func() {
				<-sem
				wg.Done()
			}() // release

			// Check cancellation before starting work
			select {
			case <-ctx.Done():
				results <- pushResult{name: wf.Name, err: ctx.Err()}
				return
			default:
			}

			filePath := opts.FilePaths[wf.Name]

			// Create layer from file
			layer, err := tarball.LayerFromFile(filePath, tarball.WithMediaType(MediaTypeWeightsLayer))
			if err != nil {
				sendProgress(WeightsPushProgress{Name: wf.Name, Done: true, Err: err})
				results <- pushResult{name: wf.Name, err: fmt.Errorf("create layer for %s: %w", wf.Name, err)}
				return
			}

			// Get size for progress tracking
			size, _ := layer.Size()
			sendProgress(WeightsPushProgress{Name: wf.Name, Total: size})

			// Create progress channel for this upload, autoclosed by WriteLayer
			var (
				progressCh   = make(chan v1.Update, 100)
				progressDone = make(chan struct{})
			)

			// Start goroutine to forward progress updates
			go func() {
				defer close(progressDone)
				for update := range progressCh {
					sendProgress(WeightsPushProgress{
						Name:     wf.Name,
						Complete: update.Complete,
						Total:    update.Total,
					})
				}
			}()

			// Build retry configuration if callback is provided
			var retryConfig *registry.RetryConfig
			if opts.RetryFn != nil || opts.RetryBackoff != nil {
				retryConfig = &registry.RetryConfig{
					Backoff: opts.RetryBackoff,
				}
				if opts.RetryFn != nil {
					retryConfig.OnRetry = func(event registry.RetryEvent) bool {
						return opts.RetryFn(WeightsRetryEvent{
							Name:        wf.Name,
							Attempt:     event.Attempt,
							MaxAttempts: event.MaxAttempts,
							Err:         event.Err,
							NextRetryIn: event.NextRetryIn,
						})
					}
				}
			}

			// Push layer to registry with progress and retry support
			err = p.registry.WriteLayer(ctx, registry.WriteLayerOptions{
				Repo:       opts.Repo,
				Layer:      layer,
				ProgressCh: progressCh,
				Retry:      retryConfig,
			})
			if err != nil {
				sendProgress(WeightsPushProgress{Name: wf.Name, Done: true, Err: err})
				results <- pushResult{name: wf.Name, err: fmt.Errorf("push layer %s: %w", wf.Name, err)}
				return
			}

			sendProgress(WeightsPushProgress{Name: wf.Name, Complete: size, Total: size, Done: true})

			// Ensure channel is closed even on error
			select {
			case <-progressDone:
				// Already closed by WriteLayer
			default:
				close(progressCh)
				<-progressDone
			}

			digest, _ := layer.Digest()
			results <- pushResult{name: wf.Name, digest: digest.String(), size: size}
		}(wf)
	}

	// Wait for all goroutines and close results channel
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	result := &WeightsPushResult{
		Files: make([]WeightPushFileResult, 0, len(opts.Lock.Files)),
	}

	for r := range results {
		result.Files = append(result.Files, WeightPushFileResult{
			Name:   r.name,
			Digest: r.digest,
			Size:   r.size,
			Err:    r.err,
		})
		if r.err == nil {
			result.TotalSize += r.size
		}
	}

	return result, nil
}
