package docker

import (
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

// isRepositoryNotFoundError checks if the error indicates that the repository
// doesn't exist on the registry. This typically means the model hasn't been
// created on Replicate yet.
func isRepositoryNotFoundError(err error) bool {
	msg := err.Error()
	// NAME_UNKNOWN is an OCI registry error code meaning "repository name not known to registry"
	return strings.Contains(msg, "NAME_UNKNOWN")
}

func isMissingDeviceDriverError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "could not select device driver") ||
		strings.Contains(msg, "nvidia-container-cli: initialization error")
}
