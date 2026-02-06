package registry

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"syscall"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

//nolint:staticcheck // ST1012: exported API, renaming would be breaking change
var NotFoundError = errors.New("image reference not found")

type RegistryClient struct{}

func NewRegistryClient() Client {
	return &RegistryClient{}
}

func (c *RegistryClient) Inspect(ctx context.Context, imageRef string, platform *Platform) (*ManifestResult, error) {
	ref, err := name.ParseReference(imageRef, name.Insecure)
	if err != nil {
		return nil, fmt.Errorf("parsing reference: %w", err)
	}

	desc, err := remote.Get(ref,
		remote.WithContext(ctx),
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
		// TODO[md]: map platform to remote.WithPlatform if necessary:
		// remote.WithPlatform(...)
	)
	if err != nil {
		if checkError(err, transport.ManifestUnknownErrorCode, transport.NameUnknownErrorCode) {
			return nil, NotFoundError
		}

		return nil, fmt.Errorf("fetching descriptor: %w", err)
	}

	mediaType := desc.MediaType

	if platform == nil {
		switch mediaType {
		case types.OCIImageIndex, types.DockerManifestList:
			idx, err := desc.ImageIndex()
			if err != nil {
				return nil, fmt.Errorf("loading image index: %w", err)
			}
			indexManifest, err := idx.IndexManifest()
			if err != nil {
				return nil, fmt.Errorf("getting index manifest: %w", err)
			}
			result := &ManifestResult{
				SchemaVersion: indexManifest.SchemaVersion,
				MediaType:     string(mediaType),
			}
			for _, m := range indexManifest.Manifests {
				result.Manifests = append(result.Manifests, PlatformManifest{
					Digest:       m.Digest.String(),
					OS:           m.Platform.OS,
					Architecture: m.Platform.Architecture,
					Variant:      m.Platform.Variant,
					Annotations:  m.Annotations,
				})
			}
			// For indexes, pick a default image to get labels from.
			// Prefer linux/amd64, otherwise use the first manifest.
			defaultImg, err := pickDefaultImage(ctx, ref, indexManifest)
			if err != nil {
				return nil, fmt.Errorf("failed to read image config from index: %w", err)
			}
			configFile, err := defaultImg.ConfigFile()
			if err != nil {
				return nil, fmt.Errorf("failed to get image config: %w", err)
			}
			result.Labels = configFile.Config.Labels
			return result, nil

		case types.OCIManifestSchema1, types.DockerManifestSchema2:
			img, err := desc.Image()
			if err != nil {
				return nil, fmt.Errorf("loading image: %w", err)
			}
			manifest, err := img.Manifest()
			if err != nil {
				return nil, fmt.Errorf("getting manifest: %w", err)
			}
			configFile, err := img.ConfigFile()
			if err != nil {
				return nil, fmt.Errorf("getting config file: %w", err)
			}
			result := &ManifestResult{
				SchemaVersion: manifest.SchemaVersion,
				MediaType:     string(mediaType),
				Config:        manifest.Config.Digest.String(),
				Labels:        configFile.Config.Labels,
			}
			for _, layer := range manifest.Layers {
				result.Layers = append(result.Layers, layer.Digest.String())
			}
			return result, nil
		default:
			return nil, fmt.Errorf("unsupported media type: %s", mediaType)
		}
	}

	// platform is set, we expect a manifest list or error
	if mediaType != types.OCIImageIndex && mediaType != types.DockerManifestList {
		return nil, fmt.Errorf("image is not a manifest list but platform was specified")
	}

	idx, err := desc.ImageIndex()
	if err != nil {
		return nil, fmt.Errorf("loading image index: %w", err)
	}
	indexManifest, err := idx.IndexManifest()
	if err != nil {
		return nil, fmt.Errorf("getting index manifest: %w", err)
	}

	var matchedDigest string
	for _, m := range indexManifest.Manifests {
		if m.Platform.OS == platform.OS &&
			m.Platform.Architecture == platform.Architecture &&
			m.Platform.Variant == platform.Variant {
			matchedDigest = m.Digest.String()
			break
		}
	}

	if matchedDigest == "" {
		return nil, fmt.Errorf("platform not found in manifest list")
	}

	digestRef, err := name.NewDigest(ref.Context().Name() + "@" + matchedDigest)
	if err != nil {
		return nil, fmt.Errorf("creating digest ref: %w", err)
	}
	manifestDesc, err := remote.Get(digestRef,
		remote.WithContext(ctx),
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
	)
	if err != nil {
		return nil, fmt.Errorf("fetching platform manifest: %w", err)
	}
	img, err := manifestDesc.Image()
	if err != nil {
		return nil, fmt.Errorf("loading platform image: %w", err)
	}
	manifest, err := img.Manifest()
	if err != nil {
		return nil, fmt.Errorf("getting manifest: %w", err)
	}
	configFile, err := img.ConfigFile()
	if err != nil {
		return nil, fmt.Errorf("getting config file: %w", err)
	}
	result := &ManifestResult{
		SchemaVersion: manifest.SchemaVersion,
		MediaType:     string(manifestDesc.MediaType),
		Config:        manifest.Config.Digest.String(),
		Labels:        configFile.Config.Labels,
	}
	for _, layer := range manifest.Layers {
		result.Layers = append(result.Layers, layer.Digest.String())
	}
	return result, nil
}

func (c *RegistryClient) GetImage(ctx context.Context, imageRef string, platform *Platform) (v1.Image, error) {
	ref, err := name.ParseReference(imageRef, name.Insecure)
	if err != nil {
		return nil, fmt.Errorf("parsing reference: %w", err)
	}

	desc, err := remote.Get(ref,
		remote.WithContext(ctx),
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
	)
	if err != nil {
		return nil, fmt.Errorf("fetching descriptor: %w", err)
	}

	mediaType := desc.MediaType

	// If no platform is specified and it's a single image, return it directly
	if platform == nil {
		switch mediaType {
		case types.OCIManifestSchema1, types.DockerManifestSchema2:
			return desc.Image()
		case types.OCIImageIndex, types.DockerManifestList:
			return nil, fmt.Errorf("platform must be specified for multi-platform image")
		default:
			return nil, fmt.Errorf("unsupported media type: %s", mediaType)
		}
	}

	// For platform-specific requests, we need to handle manifest lists
	if mediaType != types.OCIImageIndex && mediaType != types.DockerManifestList {
		return nil, fmt.Errorf("image is not a manifest list but platform was specified")
	}

	idx, err := desc.ImageIndex()
	if err != nil {
		return nil, fmt.Errorf("loading image index: %w", err)
	}

	indexManifest, err := idx.IndexManifest()
	if err != nil {
		return nil, fmt.Errorf("getting index manifest: %w", err)
	}

	// Find the matching platform manifest
	var matchedDigest string
	for _, m := range indexManifest.Manifests {
		if m.Platform.OS == platform.OS &&
			m.Platform.Architecture == platform.Architecture &&
			m.Platform.Variant == platform.Variant {
			matchedDigest = m.Digest.String()
			break
		}
	}

	if matchedDigest == "" {
		return nil, fmt.Errorf("platform not found in manifest list")
	}

	// Get the image for the matched digest
	digestRef, err := name.NewDigest(ref.Context().Name() + "@" + matchedDigest)
	if err != nil {
		return nil, fmt.Errorf("creating digest ref: %w", err)
	}

	manifestDesc, err := remote.Get(digestRef,
		remote.WithContext(ctx),
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
	)
	if err != nil {
		return nil, fmt.Errorf("fetching platform manifest: %w", err)
	}

	return manifestDesc.Image()
}

// GetDescriptor returns the OCI descriptor for an image reference using a HEAD request.
// This is lightweight — it does not download the full manifest or image layers.
func (c *RegistryClient) GetDescriptor(ctx context.Context, imageRef string) (v1.Descriptor, error) {
	ref, err := name.ParseReference(imageRef, name.Insecure)
	if err != nil {
		return v1.Descriptor{}, fmt.Errorf("parsing reference: %w", err)
	}

	desc, err := remote.Head(ref,
		remote.WithContext(ctx),
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
	)
	if err != nil {
		return v1.Descriptor{}, fmt.Errorf("head request for %s: %w", imageRef, err)
	}

	return *desc, nil
}

func (c *RegistryClient) Exists(ctx context.Context, imageRef string) (bool, error) {
	if _, err := c.Inspect(ctx, imageRef, nil); err != nil {
		if errors.Is(err, NotFoundError) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func checkError(err error, codes ...transport.ErrorCode) bool {
	if err == nil {
		return false
	}

	var e *transport.Error
	if errors.As(err, &e) {
		for _, diagnosticErr := range e.Errors {
			for _, code := range codes {
				if diagnosticErr.Code == code {
					return true
				}
			}
		}
	}
	return false
}

// PushImage pushes a single image to a registry.
func (c *RegistryClient) PushImage(ctx context.Context, ref string, img v1.Image) error {
	parsedRef, err := name.ParseReference(ref, name.Insecure)
	if err != nil {
		return fmt.Errorf("parsing reference: %w", err)
	}

	opts := []remote.Option{
		remote.WithContext(ctx),
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
	}

	if err := remote.Write(parsedRef, img, opts...); err != nil {
		return fmt.Errorf("pushing image %s: %w", ref, err)
	}

	return nil
}

// PushIndex pushes an OCI Image Index to a registry.
func (c *RegistryClient) PushIndex(ctx context.Context, ref string, idx v1.ImageIndex) error {
	parsedRef, err := name.ParseReference(ref, name.Insecure)
	if err != nil {
		return fmt.Errorf("parsing reference: %w", err)
	}

	opts := []remote.Option{
		remote.WithContext(ctx),
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
	}

	if err := remote.WriteIndex(parsedRef, idx, opts...); err != nil {
		return fmt.Errorf("pushing index %s: %w", ref, err)
	}

	return nil
}

// DefaultRetryBackoff returns the default retry backoff configuration for weight pushes.
// It retries 5 times with exponential backoff starting at 2 seconds.
func DefaultRetryBackoff() remote.Backoff {
	return remote.Backoff{
		Duration: 2 * time.Second,
		Factor:   2.0,
		Jitter:   0.1,
		Steps:    5,
	}
}

// isRetryableError determines if an error should trigger a retry.
// This matches the go-containerregistry default retry predicate plus additional cases.
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Check for context cancellation - don't retry these
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	// Check for temporary errors (network issues, etc.)
	var tempErr interface{ Temporary() bool }
	if errors.As(err, &tempErr) && tempErr.Temporary() {
		return true
	}

	// Check for common transient errors
	if errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, net.ErrClosed) {
		return true
	}

	// Check for retryable HTTP status codes in transport errors
	var transportErr *transport.Error
	if errors.As(err, &transportErr) {
		switch transportErr.StatusCode {
		case http.StatusRequestTimeout,
			http.StatusInternalServerError,
			http.StatusBadGateway,
			http.StatusServiceUnavailable,
			http.StatusGatewayTimeout,
			499, // nginx-specific, client closed request
			522: // Cloudflare-specific, connection timeout
			return true
		}
	}

	// Check for network operation errors (connection refused, timeout, etc.)
	var netErr *net.OpError
	if errors.As(err, &netErr) {
		return true
	}

	// Check for DNS errors
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return dnsErr.Temporary()
	}

	return false
}

// WriteLayer pushes a single layer with retry and optional progress reporting.
// This implements retry at the application level with callbacks for CLI feedback.
// Unlike the standard remote.WriteLayer, this implementation performs multipart uploads
// using Content-Range headers to upload the blob in chunks.
func (c *RegistryClient) WriteLayer(ctx context.Context, opts WriteLayerOptions) error {
	parsedRepo, err := name.NewRepository(opts.Repo, name.Insecure)
	if err != nil {
		return fmt.Errorf("parsing repository: %w", err)
	}

	// Determine retry configuration
	backoff := DefaultRetryBackoff()
	if opts.Retry != nil && opts.Retry.Backoff != nil {
		backoff = *opts.Retry.Backoff
	}

	var lastErr error
	currentDelay := backoff.Duration

	for attempt := 1; attempt <= backoff.Steps; attempt++ {
		// Check for context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Attempt the push using custom multipart upload
		err := c.writeLayerMultipart(ctx, parsedRepo, opts)
		if err == nil {
			return nil // Success
		}

		lastErr = err

		// Check if this error is retryable
		if !isRetryableError(err) {
			return fmt.Errorf("pushing layer to %s: %w", opts.Repo, err)
		}

		// Don't retry if this was the last attempt
		if attempt >= backoff.Steps {
			break
		}

		// Calculate next delay with jitter
		nextDelay := currentDelay
		if backoff.Jitter > 0 {
			// Simple jitter: add up to jitter% of the delay
			jitterAmount := time.Duration(float64(currentDelay) * backoff.Jitter)
			nextDelay = currentDelay + jitterAmount
		}

		// Invoke retry callback if configured
		if opts.Retry != nil && opts.Retry.OnRetry != nil {
			event := RetryEvent{
				Attempt:     attempt,
				MaxAttempts: backoff.Steps,
				Err:         err,
				NextRetryIn: nextDelay,
			}
			if !opts.Retry.OnRetry(event) {
				// Callback returned false, abort retrying
				return fmt.Errorf("pushing layer to %s (retry aborted): %w", opts.Repo, err)
			}
		}

		// Wait before retrying
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(nextDelay):
		}

		// Update delay for next iteration
		currentDelay = time.Duration(float64(currentDelay) * backoff.Factor)
	}

	return fmt.Errorf("pushing layer to %s (after %d attempts): %w", opts.Repo, backoff.Steps, lastErr)
}

// writeLayerMultipart uploads a layer using multipart uploads with Content-Range headers.
// This is a custom implementation that supports chunked uploads compatible with the
// server-side code provided.
func (c *RegistryClient) writeLayerMultipart(ctx context.Context, repo name.Repository, opts WriteLayerOptions) error {
	// Get layer metadata
	digest, err := opts.Layer.Digest()
	if err != nil {
		return fmt.Errorf("getting layer digest: %w", err)
	}

	size, err := opts.Layer.Size()
	if err != nil {
		return fmt.Errorf("getting layer size: %w", err)
	}

	// Create authenticated HTTP client
	auth, err := authn.Resolve(ctx, authn.DefaultKeychain, repo)
	if err != nil {
		return fmt.Errorf("resolving auth: %w", err)
	}

	scopes := []string{repo.Scope(transport.PushScope)}
	tr, err := transport.NewWithContext(ctx, repo.Registry, auth, http.DefaultTransport, scopes)
	if err != nil {
		return fmt.Errorf("creating transport: %w", err)
	}

	client := &http.Client{Transport: tr}

	// Check if blob already exists
	exists, err := c.checkBlobExists(ctx, client, repo, digest)
	if err != nil {
		return fmt.Errorf("checking blob existence: %w", err)
	}
	if exists {
		if opts.ProgressCh != nil {
			opts.ProgressCh <- v1.Update{Complete: size, Total: size}
		}
		return nil
	}

	// Initiate upload
	location, err := c.initiateUpload(ctx, client, repo)
	if err != nil {
		return fmt.Errorf("initiating upload: %w", err)
	}

	// Upload the blob in chunks
	finalLocation, err := c.uploadBlobChunks(ctx, client, repo, opts.Layer, location, size, opts.ProgressCh)
	if err != nil {
		return fmt.Errorf("uploading blob chunks: %w", err)
	}

	// Commit the upload using the final location (which contains updated state hash)
	err = c.commitUpload(ctx, client, finalLocation, digest)
	if err != nil {
		return fmt.Errorf("committing upload: %w", err)
	}

	return nil
}

// checkBlobExists checks if a blob already exists in the repository.
func (c *RegistryClient) checkBlobExists(ctx context.Context, client *http.Client, repo name.Repository, digest v1.Hash) (bool, error) {
	u := url.URL{
		Scheme: repo.Scheme(),
		Host:   repo.RegistryStr(),
		Path:   fmt.Sprintf("/v2/%s/blobs/%s", repo.RepositoryStr(), digest.String()),
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, u.String(), nil)
	if err != nil {
		return false, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if err := transport.CheckError(resp, http.StatusOK, http.StatusNotFound); err != nil {
		return false, err
	}

	return resp.StatusCode == http.StatusOK, nil
}

// initiateUpload initiates a blob upload and returns the upload location URL.
func (c *RegistryClient) initiateUpload(ctx context.Context, client *http.Client, repo name.Repository) (string, error) {
	u := url.URL{
		Scheme: repo.Scheme(),
		Host:   repo.RegistryStr(),
		Path:   fmt.Sprintf("/v2/%s/blobs/uploads/", repo.RepositoryStr()),
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if err := transport.CheckError(resp, http.StatusAccepted); err != nil {
		return "", err
	}

	loc := resp.Header.Get("Location")
	if loc == "" {
		return "", errors.New("missing Location header in initiate upload response")
	}

	// Resolve relative URLs
	locURL, err := url.Parse(loc)
	if err != nil {
		return "", fmt.Errorf("parsing location URL: %w", err)
	}

	baseURL := url.URL{
		Scheme: repo.Scheme(),
		Host:   repo.RegistryStr(),
	}

	return baseURL.ResolveReference(locURL).String(), nil
}

// uploadBlobChunks uploads a blob using either multipart or single-part upload depending on server support.
// The repo parameter is needed to restart the upload session if multipart fails.
// Returns the final upload location URL which must be used for committing the upload.
func (c *RegistryClient) uploadBlobChunks(ctx context.Context, client *http.Client, repo name.Repository, layer v1.Layer, location string, totalSize int64, progressCh chan<- v1.Update) (string, error) {
	// Multipart upload settings:
	// - Threshold: Use multipart only for blobs larger than 50MB (avoids MPU overhead for smaller files)
	// - Chunk size: 25MB per chunk (good balance for object stores and typical network conditions)
	const multipartThreshold = 50 * 1024 * 1024
	const chunkSize = 25 * 1024 * 1024

	if totalSize > multipartThreshold {
		finalLocation, newLocation, fallback, err := c.tryMultipartWithFallback(ctx, client, repo, layer, location, totalSize, chunkSize, progressCh)
		if err != nil {
			return "", err
		}
		if !fallback {
			return finalLocation, nil
		}
		// Multipart not supported, continue with single-part using the new location
		location = newLocation
	}

	// Single-part upload for small blobs or servers that don't support multipart
	blob, err := layer.Compressed()
	if err != nil {
		return "", fmt.Errorf("getting compressed blob: %w", err)
	}
	defer blob.Close()

	finalLocation, err := c.uploadBlobSingle(ctx, client, location, blob, totalSize, progressCh)
	if err != nil {
		return "", err
	}
	return finalLocation, nil
}

// tryMultipartWithFallback attempts multipart upload and handles fallback if not supported.
// Returns (finalLocation, newLocation, fallback, error):
//   - If multipart succeeds: (finalLocation, "", false, nil)
//   - If multipart not supported: ("", newLocation, true, nil) - caller should use single-part with newLocation
//   - If error: ("", "", false, error)
func (c *RegistryClient) tryMultipartWithFallback(ctx context.Context, client *http.Client, repo name.Repository, layer v1.Layer, location string, totalSize int64, chunkSize int64, progressCh chan<- v1.Update) (finalLocation string, newLocation string, fallback bool, err error) {
	blob, err := layer.Compressed()
	if err != nil {
		return "", "", false, fmt.Errorf("getting compressed blob: %w", err)
	}
	defer blob.Close()

	finalLocation, err = c.tryMultipartUpload(ctx, client, location, blob, totalSize, chunkSize, progressCh)
	if err == nil {
		return finalLocation, "", false, nil
	}

	// Check if error indicates multipart not supported
	var transportErr *transport.Error
	if errors.As(err, &transportErr) && (transportErr.StatusCode == http.StatusRequestedRangeNotSatisfiable ||
		transportErr.StatusCode == http.StatusBadRequest) {
		// Multipart not supported - restart upload session for single-part fallback
		newLocation, err = c.initiateUpload(ctx, client, repo)
		if err != nil {
			return "", "", false, fmt.Errorf("restarting upload after multipart failure: %w", err)
		}
		return "", newLocation, true, nil
	}

	return "", "", false, err
}

// tryMultipartUpload attempts to upload using Content-Range headers.
// Returns the final location or an error.
func (c *RegistryClient) tryMultipartUpload(ctx context.Context, client *http.Client, location string, blob io.Reader, totalSize int64, chunkSize int64, progressCh chan<- v1.Update) (string, error) {
	var uploaded int64
	buffer := make([]byte, chunkSize)
	currentLocation := location

	for uploaded < totalSize {
		// Read the next chunk
		n, err := io.ReadFull(blob, buffer)
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
			return "", fmt.Errorf("reading blob: %w", err)
		}
		if n == 0 {
			break
		}

		chunk := buffer[:n]
		start := uploaded
		end := uploaded + int64(n) - 1 // Range is inclusive

		// Upload the chunk with Content-Range (progress is reported within uploadChunk)
		newLocation, err := c.uploadChunk(ctx, client, currentLocation, chunk, start, end, totalSize, progressCh)
		if err != nil {
			return "", err
		}

		// Update location for next chunk (server may change it)
		if newLocation != "" {
			currentLocation = newLocation
		}

		uploaded += int64(n)

		// Check for context cancellation
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
	}

	return currentLocation, nil
}

// uploadBlobSingle uploads the entire blob in one request without Content-Range headers.
func (c *RegistryClient) uploadBlobSingle(ctx context.Context, client *http.Client, location string, blob io.Reader, totalSize int64, progressCh chan<- v1.Update) (string, error) {
	// Wrap the reader to report progress
	var uploaded int64
	reader := &progressReader{
		reader: blob,
		onRead: func(n int) {
			uploaded += int64(n)
			if progressCh != nil {
				// Cap at totalSize defensively
				complete := min(uploaded, totalSize)
				select {
				case progressCh <- v1.Update{Complete: complete, Total: totalSize}:
				default:
					// Don't block if channel is full
				}
			}
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, location, reader)
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = totalSize

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if err := transport.CheckError(resp, http.StatusAccepted, http.StatusNoContent, http.StatusCreated); err != nil {
		return "", err
	}

	// Return the updated Location header — the registry includes upload state
	// that commitUpload needs for the final PUT.
	if loc := resp.Header.Get("Location"); loc != "" {
		locURL, parseErr := url.Parse(loc)
		if parseErr == nil {
			baseURL := url.URL{Scheme: "http", Host: req.URL.Host}
			if req.URL.Scheme != "" {
				baseURL.Scheme = req.URL.Scheme
			}
			return baseURL.ResolveReference(locURL).String(), nil
		}
	}
	return location, nil
}

// uploadChunk uploads a single chunk of a blob with Content-Range header.
// Returns the new location URL if the server returns one.
// If progressCh is provided, progress updates are sent as bytes are uploaded.
// Progress updates occur approximately every 32-64KB based on HTTP client buffer size.
func (c *RegistryClient) uploadChunk(ctx context.Context, client *http.Client, location string, chunk []byte, start, end int64, totalSize int64, progressCh chan<- v1.Update) (string, error) {
	// Wrap the chunk reader to report progress as bytes are written
	var reader io.Reader
	if progressCh != nil {
		var chunkUploaded int64
		reader = &progressReader{
			reader: bytes.NewReader(chunk),
			onRead: func(n int) {
				chunkUploaded += int64(n)
				// Cap at totalSize defensively
				complete := min(start+chunkUploaded, totalSize)
				select {
				case progressCh <- v1.Update{Complete: complete, Total: totalSize}:
				default:
					// Don't block if channel is full
				}
			},
		}
	} else {
		reader = bytes.NewReader(chunk)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, location, reader)
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Content-Length", strconv.FormatInt(int64(len(chunk)), 10))
	req.Header.Set("Content-Range", fmt.Sprintf("%d-%d", start, end))

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if err := transport.CheckError(resp, http.StatusAccepted, http.StatusNoContent, http.StatusCreated); err != nil {
		return "", err
	}

	// Get the new location for the next chunk
	newLocation := resp.Header.Get("Location")
	if newLocation != "" {
		// Resolve relative URLs
		locURL, err := url.Parse(newLocation)
		if err != nil {
			return "", fmt.Errorf("parsing location URL: %w", err)
		}

		// Parse the original location to get the base URL
		origURL, err := url.Parse(location)
		if err != nil {
			return "", fmt.Errorf("parsing original location URL: %w", err)
		}

		return origURL.ResolveReference(locURL).String(), nil
	}

	return "", nil
}

// progressReader wraps an io.Reader to report progress.
type progressReader struct {
	reader io.Reader
	onRead func(int)
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	if n > 0 {
		pr.onRead(n)
	}
	return n, err
}

// commitUpload finalizes the upload by sending a PUT request with the digest.
func (c *RegistryClient) commitUpload(ctx context.Context, client *http.Client, location string, digest v1.Hash) error {
	u, err := url.Parse(location)
	if err != nil {
		return fmt.Errorf("parsing location URL: %w", err)
	}

	// Add digest query parameter
	q := u.Query()
	q.Set("digest", digest.String())
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u.String(), nil)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return transport.CheckError(resp, http.StatusCreated)
}

// pickDefaultImage selects an image from a manifest index to use for fetching labels.
// Prefers linux/amd64, otherwise returns the first image manifest.
// Returns an error if no suitable image is found or if fetching fails.
func pickDefaultImage(ctx context.Context, ref name.Reference, idx *v1.IndexManifest) (v1.Image, error) {
	var targetDigest string

	// First, look for linux/amd64
	for _, m := range idx.Manifests {
		if m.Platform != nil && m.Platform.OS == "linux" && m.Platform.Architecture == "amd64" {
			targetDigest = m.Digest.String()
			break
		}
	}

	// Fall back to first manifest
	if targetDigest == "" && len(idx.Manifests) > 0 {
		targetDigest = idx.Manifests[0].Digest.String()
	}

	if targetDigest == "" {
		return nil, fmt.Errorf("index for %s contains no manifests", ref.String())
	}

	digestRef, err := name.NewDigest(ref.Context().Name()+"@"+targetDigest, name.Insecure)
	if err != nil {
		return nil, fmt.Errorf("failed to create digest reference: %w", err)
	}

	desc, err := remote.Get(digestRef,
		remote.WithContext(ctx),
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch image %s: %w", digestRef.String(), err)
	}

	img, err := desc.Image()
	if err != nil {
		return nil, fmt.Errorf("failed to load image %s: %w", digestRef.String(), err)
	}

	return img, nil
}
