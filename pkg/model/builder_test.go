package model

import (
	"context"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/stretchr/testify/require"
)

// mockBuilder is a test double that implements the Builder interface.
type mockBuilder struct {
	buildFn func(ctx context.Context, spec ArtifactSpec) (Artifact, error)
}

func (m *mockBuilder) Build(ctx context.Context, spec ArtifactSpec) (Artifact, error) {
	return m.buildFn(ctx, spec)
}

func TestBuilderInterface_Satisfiable(t *testing.T) {
	// Compile-time check: mockBuilder satisfies Builder.
	var _ Builder = &mockBuilder{}

	// Runtime check: a mock builder can be called and returns an artifact.
	mb := &mockBuilder{
		buildFn: func(_ context.Context, spec ArtifactSpec) (Artifact, error) {
			return NewImageArtifact(spec.Name(), v1.Descriptor{}, "test-ref"), nil
		},
	}

	artifact, err := mb.Build(context.Background(), NewImageSpec("test", "test-image"))
	require.NoError(t, err)
	require.Equal(t, "test", artifact.Name())
}
