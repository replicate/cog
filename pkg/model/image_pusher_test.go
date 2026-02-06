package model

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// =============================================================================
// ImagePusher.PushArtifact tests
// =============================================================================

func TestImagePusher_PushArtifact_PushesImageViaDocker(t *testing.T) {
	var pushedRef string
	docker := &mockDocker{
		pushFunc: func(ctx context.Context, ref string) error {
			pushedRef = ref
			return nil
		},
	}

	pusher := NewImagePusher(docker)
	artifact := &ImageArtifact{Reference: "r8.im/user/model:latest"}

	err := pusher.PushArtifact(context.Background(), artifact)

	require.NoError(t, err)
	require.Equal(t, "r8.im/user/model:latest", pushedRef)
}

func TestImagePusher_PushArtifact_ReturnsErrorForNilArtifact(t *testing.T) {
	docker := &mockDocker{}
	pusher := NewImagePusher(docker)

	err := pusher.PushArtifact(context.Background(), nil)

	require.Error(t, err)
	require.Contains(t, err.Error(), "artifact is nil")
}

func TestImagePusher_PushArtifact_ReturnsErrorForEmptyReference(t *testing.T) {
	docker := &mockDocker{}
	pusher := NewImagePusher(docker)

	err := pusher.PushArtifact(context.Background(), &ImageArtifact{Reference: ""})

	require.Error(t, err)
	require.Contains(t, err.Error(), "image has no reference")
}

func TestImagePusher_PushArtifact_PropagatesDockerPushError(t *testing.T) {
	docker := &mockDocker{
		pushFunc: func(ctx context.Context, ref string) error {
			return errors.New("unauthorized: authentication required")
		},
	}

	pusher := NewImagePusher(docker)
	artifact := &ImageArtifact{Reference: "r8.im/user/model:latest"}

	err := pusher.PushArtifact(context.Background(), artifact)

	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
}

// =============================================================================
// ImagePusher.Push (legacy Model-based interface) tests
// =============================================================================

func TestImagePusher_Push_DelegatesToPushArtifact(t *testing.T) {
	var pushedRef string
	docker := &mockDocker{
		pushFunc: func(ctx context.Context, ref string) error {
			pushedRef = ref
			return nil
		},
	}

	pusher := NewImagePusher(docker)
	m := &Model{
		Image: &ImageArtifact{Reference: "r8.im/user/model:latest"},
	}

	err := pusher.Push(context.Background(), m, PushOptions{})

	require.NoError(t, err)
	require.Equal(t, "r8.im/user/model:latest", pushedRef)
}

func TestImagePusher_Push_ReturnsErrorWhenImageNil(t *testing.T) {
	docker := &mockDocker{}
	pusher := NewImagePusher(docker)
	m := &Model{Image: nil}

	err := pusher.Push(context.Background(), m, PushOptions{})

	require.Error(t, err)
	require.Contains(t, err.Error(), "artifact is nil")
}

func TestImagePusher_Push_ReturnsErrorWhenReferenceEmpty(t *testing.T) {
	docker := &mockDocker{}
	pusher := NewImagePusher(docker)
	m := &Model{Image: &ImageArtifact{Reference: ""}}

	err := pusher.Push(context.Background(), m, PushOptions{})

	require.Error(t, err)
	require.Contains(t, err.Error(), "image has no reference")
}

func TestImagePusher_Push_PropagatesDockerError(t *testing.T) {
	docker := &mockDocker{
		pushFunc: func(ctx context.Context, ref string) error {
			return errors.New("unauthorized: authentication required")
		},
	}

	pusher := NewImagePusher(docker)
	m := &Model{Image: &ImageArtifact{Reference: "r8.im/user/model:latest"}}

	err := pusher.Push(context.Background(), m, PushOptions{})

	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
}
