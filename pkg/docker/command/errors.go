package command

import (
	"errors"
	"fmt"
)

// NotFoundError represents “object <ref> wasn’t found” inside the Docker engine.
type NotFoundError struct {
	// Ref is a unique identifier, such as an image reference, container ID, etc.
	Ref string
	// Object is the ref type, such as "container", "image", "volume", etc.
	Object string
}

func (e *NotFoundError) Error() string {
	objType := e.Object
	if objType == "" {
		objType = "object"
	}
	return fmt.Sprintf("%s not found: %q", objType, e.Ref)
}

func (e *NotFoundError) Is(target error) bool {
	_, ok := target.(*NotFoundError)
	return ok
}

func IsNotFoundError(err error) bool {
	return errors.Is(err, &NotFoundError{})
}

var ErrAuthorizationFailed = errors.New("authorization failed")
