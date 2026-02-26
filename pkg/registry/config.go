package registry

import (
	"os"
	"strconv"
)

const (
	// DefaultChunkSize is the size (in bytes) of each chunk in a multipart upload.
	// This is used as a fallback when the registry does not advertise chunk size
	// limits via OCI-Chunk-Min-Length / OCI-Chunk-Max-Length headers.
	// 96 MB stays under common CDN/proxy request body limits while still being
	// large enough to reduce HTTP round-trips for multi-GB files.
	DefaultChunkSize = 96 * 1024 * 1024 // 96 MB

	// DefaultMultipartThreshold is the minimum blob size (in bytes) before using multipart upload.
	// Blobs smaller than this are uploaded in a single request to avoid multipart overhead.
	// Set higher than DefaultChunkSize so that blobs that would fit in a single chunk
	// are uploaded in one request, avoiding unnecessary multipart overhead.
	DefaultMultipartThreshold = 128 * 1024 * 1024 // 128 MB

	// chunkSizeMargin is subtracted from the server's OCI-Chunk-Max-Length to stay
	// safely under the limit (e.g. for HTTP framing overhead).
	chunkSizeMargin = 64 * 1024 // 64 KB

	// envPushDefaultChunkSize sets the default chunk size for multipart uploads.
	// This is only used when the registry does not advertise OCI-Chunk-Max-Length.
	// When the registry does advertise a maximum, the server's limit takes precedence.
	envPushDefaultChunkSize = "COG_PUSH_DEFAULT_CHUNK_SIZE"

	// envMultipartThreshold overrides the minimum blob size for multipart uploads.
	envMultipartThreshold = "COG_PUSH_MULTIPART_THRESHOLD"
)

// getDefaultChunkSize returns the client-configured default chunk size for multipart uploads.
// This is used as a fallback when the registry does not advertise chunk size limits.
func getDefaultChunkSize() int64 {
	if v := os.Getenv(envPushDefaultChunkSize); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return DefaultChunkSize
}

// getMultipartThreshold returns the minimum blob size for multipart uploads.
func getMultipartThreshold() int64 {
	if v := os.Getenv(envMultipartThreshold); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return DefaultMultipartThreshold
}
