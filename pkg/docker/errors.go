package docker

import "strings"

// Error messages vary between different backends (dockerd, containerd, podman, orbstack, etc) or even versions of docker.
// These helpers normalize the check so callers can handle situations without worrying about the underlying implementation.
// Yes, it's gross, but whattaya gonna do

func isTagNotFoundError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "tag does not exist") ||
		strings.Contains(msg, "An image does not exist locally with the tag")
}

func isImageNotFoundError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "image does not exist") ||
		strings.Contains(msg, "No such image")
}

func isContainerNotFoundError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "container does not exist") ||
		strings.Contains(msg, "No such container")
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
