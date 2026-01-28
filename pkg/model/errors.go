package model

import "errors"

// Sentinel errors for Resolver operations.
var (
	// ErrNotCogModel indicates the image exists but is not a valid Cog model.
	// This occurs when the image lacks the required org.cogmodel.config label.
	ErrNotCogModel = errors.New("image is not a Cog model")

	// ErrNotFound indicates the image was not found in the requested location(s).
	ErrNotFound = errors.New("image not found")
)
