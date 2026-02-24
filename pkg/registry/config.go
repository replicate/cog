package registry

import (
	"os"
	"strconv"
)

const (
	// DefaultMultipartThreshold is the minimum blob size (in bytes) before using multipart upload.
	// Blobs smaller than this are uploaded in a single request to avoid multipart overhead.
	DefaultMultipartThreshold = 50 * 1024 * 1024 // 50 MB

	// DefaultChunkSize is the size (in bytes) of each chunk in a multipart upload.
	// 95 MB stays under common CDN/proxy request body limits while still being
	// large enough to reduce HTTP round-trips for multi-GB files.
	DefaultChunkSize = 95 * 1024 * 1024 // 95 MB

	// envPushChunkSize overrides the default chunk size for multipart uploads.
	envPushChunkSize = "COG_PUSH_CHUNK_SIZE"

	// envMultipartThreshold overrides the minimum blob size for multipart uploads.
	envMultipartThreshold = "COG_PUSH_MULTIPART_THRESHOLD"
)

// getChunkSize returns the configured chunk size for multipart uploads.
func getChunkSize() int64 {
	if v := os.Getenv(envPushChunkSize); v != "" {
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
