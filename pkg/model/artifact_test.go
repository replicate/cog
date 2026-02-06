package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestArtifactType_String(t *testing.T) {
	tests := []struct {
		name   string
		at     ArtifactType
		expect string
	}{
		{name: "image type", at: ArtifactTypeImage, expect: "image"},
		{name: "weight type", at: ArtifactTypeWeight, expect: "weight"},
		{name: "zero value", at: ArtifactType(0), expect: "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expect, tt.at.String())
		})
	}
}

func TestArtifactType_Values(t *testing.T) {
	// Ensure types are distinct
	require.NotEqual(t, ArtifactTypeImage, ArtifactTypeWeight)
}
