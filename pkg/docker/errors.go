package docker

import (
	"errors"
	"strings"
)

// Error messages vary between different backends (dockerd, containerd, podman, orbstack, etc) or even versions of docker.
// These helpers normalize the check so callers can handle situations without worrying about the underlying implementation.
// Yes, it's gross, but whattaya gonna do

func isTagNotFoundError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "tag does not exist") ||
		strings.Contains(msg, "An image does not exist locally with the tag")
}

func isAuthorizationFailedError(err error) bool {
	msg := err.Error()

	// registry requires auth and none were provided
	if strings.Contains(msg, "no basic auth credentials") {
		return true
	}

	// registry rejected the provided auth
	if strings.Contains(msg, "authorization failed") ||
		strings.Contains(msg, "401 Unauthorized") ||
		strings.Contains(msg, "unauthorized: authentication required") {
		return true
	}

	return false
}

func isMissingDeviceDriverError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "could not select device driver") ||
		strings.Contains(msg, "nvidia-container-cli: initialization error")
}

// isNetworkError checks if the error is a network error. This is janky and intended for use in tests only
func isNetworkError(err error) bool {
	// for both CLI and API clients, network errors are wrapped and lose the net.Error interface
	// CLI client: wrapped by exec.Command as exec.ExitError
	// API client: wrapped by JSON message stream processing
	// Sad as it may be, we rely on string matching for common network error messages

	msg := err.Error()
	networkErrorStrings := []string{
		"connection refused",
		"connection reset by peer",
		"dial tcp",
		"EOF",
		"no route to host",
		"network is unreachable",
		"server closed",
	}

	for _, errStr := range networkErrorStrings {
		if strings.Contains(msg, errStr) {
			return true
		}
	}

	// also check wrapped errors
	if unwrapped := errors.Unwrap(err); unwrapped != nil {
		return isNetworkError(unwrapped)
	}

	return false
}
