package model

import (
	"testing"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/config"
	"github.com/replicate/cog/pkg/model/weightsource"
	"github.com/replicate/cog/pkg/weights/lockfile"
)

func TestWeightSpecFromConfig_ImplementsArtifactSpec(t *testing.T) {
	spec, err := WeightSpecFromConfig(config.WeightSource{
		Name:   "my-model-weights",
		Target: "/src/weights",
		Source: &config.WeightSourceConfig{URI: "/data/weights"},
	})
	require.NoError(t, err)

	var _ ArtifactSpec = spec // compile-time interface check

	require.Equal(t, ArtifactTypeWeight, spec.Type())
	require.Equal(t, "my-model-weights", spec.Name())
}

func TestWeightSpecFromConfig_NormalizesURI(t *testing.T) {
	spec, err := WeightSpecFromConfig(config.WeightSource{
		Name:   "llama-7b",
		Target: "/src/weights/llama-7b",
		Source: &config.WeightSourceConfig{URI: "weights/llama-7b"},
	})
	require.NoError(t, err)

	require.Equal(t, "file://./weights/llama-7b", spec.URI)
	require.Equal(t, "/src/weights/llama-7b", spec.Target)
	require.Empty(t, spec.Include)
	require.Empty(t, spec.Exclude)
}

func TestWeightSpecFromConfig_CopiesIncludeExclude(t *testing.T) {
	src := &config.WeightSourceConfig{
		URI:     "weights/mw",
		Include: []string{"*.safetensors"},
		Exclude: []string{"*.onnx"},
	}
	spec, err := WeightSpecFromConfig(config.WeightSource{
		Name:   "mw",
		Target: "/src/weights/mw",
		Source: src,
	})
	require.NoError(t, err)

	require.Equal(t, []string{"*.safetensors"}, spec.Include)
	require.Equal(t, []string{"*.onnx"}, spec.Exclude)

	// Mutating the config after construction must not affect the spec.
	src.Include[0] = "changed"
	require.Equal(t, []string{"*.safetensors"}, spec.Include)
}

func TestWeightSpecFromConfig_EmptyURIError(t *testing.T) {
	_, err := WeightSpecFromConfig(config.WeightSource{Name: "w", Target: "/w"})
	require.Error(t, err)
}

func TestWeightSpecFromConfig_InvalidSchemeError(t *testing.T) {
	_, err := WeightSpecFromConfig(config.WeightSource{
		Name: "w", Target: "/w",
		Source: &config.WeightSourceConfig{URI: "bogus://nope"},
	})
	require.Error(t, err)
}

func TestWeightSpecFromLock_ExtractsIntent(t *testing.T) {
	entry := lockfile.WeightLockEntry{
		Name:   "w",
		Target: "/src/w",
		Source: lockfile.WeightLockSource{
			URI:         "file://./w",
			Fingerprint: weightsource.Fingerprint("sha256:abc"),
			Include:     []string{"*.safetensors"},
			Exclude:     []string{"*.onnx"},
			ImportedAt:  time.Now(),
		},
		Digest: "sha256:manifest",
	}

	spec := WeightSpecFromLock(entry)

	require.Equal(t, "w", spec.Name())
	require.Equal(t, "/src/w", spec.Target)
	require.Equal(t, "file://./w", spec.URI)
	require.Equal(t, []string{"*.safetensors"}, spec.Include)
	require.Equal(t, []string{"*.onnx"}, spec.Exclude)
}

func TestWeightSpec_Equal(t *testing.T) {
	base := func() *WeightSpec {
		s, err := WeightSpecFromConfig(config.WeightSource{
			Name:   "w",
			Target: "/src/w",
			Source: &config.WeightSourceConfig{
				URI:     "weights",
				Include: []string{"*.safetensors"},
				Exclude: []string{"*.onnx"},
			},
		})
		require.NoError(t, err)
		return s
	}

	require.True(t, base().Equal(base()))

	cases := []struct {
		name   string
		mutate func(*WeightSpec)
	}{
		{"target differs", func(s *WeightSpec) { s.Target = "/src/other" }},
		{"URI differs", func(s *WeightSpec) { s.URI = "file://./other" }},
		{"include differs", func(s *WeightSpec) { s.Include = []string{"*.bin"} }},
		{"exclude differs", func(s *WeightSpec) { s.Exclude = []string{"*.md"} }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := base()
			b := base()
			tc.mutate(b)
			require.False(t, a.Equal(b), "specs should differ: %s", tc.name)
		})
	}
}

func TestWeightSpec_EqualIgnoresIncludeExcludeOrder(t *testing.T) {
	// Include/Exclude are sets of glob patterns; reordering them in
	// cog.yaml must not count as drift.
	a, err := WeightSpecFromConfig(config.WeightSource{
		Name: "w", Target: "/w",
		Source: &config.WeightSourceConfig{
			URI:     "weights",
			Include: []string{"*.safetensors", "*.json"},
			Exclude: []string{"*.onnx", "*.md"},
		},
	})
	require.NoError(t, err)
	b, err := WeightSpecFromConfig(config.WeightSource{
		Name: "w", Target: "/w",
		Source: &config.WeightSourceConfig{
			URI:     "weights",
			Include: []string{"*.json", "*.safetensors"},
			Exclude: []string{"*.md", "*.onnx"},
		},
	})
	require.NoError(t, err)
	require.True(t, a.Equal(b))
}

func TestWeightSpec_EqualURINormalization(t *testing.T) {
	a, err := WeightSpecFromConfig(config.WeightSource{
		Name:   "w",
		Target: "/w",
		Source: &config.WeightSourceConfig{URI: "weights"},
	})
	require.NoError(t, err)
	b, err := WeightSpecFromConfig(config.WeightSource{
		Name:   "w",
		Target: "/w",
		Source: &config.WeightSourceConfig{URI: "file://./weights"},
	})
	require.NoError(t, err)
	require.True(t, a.Equal(b))
}

func TestWeightSpec_EqualIgnoresName(t *testing.T) {
	a, err := WeightSpecFromConfig(config.WeightSource{
		Name: "a", Target: "/w",
		Source: &config.WeightSourceConfig{URI: "weights"},
	})
	require.NoError(t, err)
	b, err := WeightSpecFromConfig(config.WeightSource{
		Name: "b", Target: "/w",
		Source: &config.WeightSourceConfig{URI: "weights"},
	})
	require.NoError(t, err)
	require.True(t, a.Equal(b))
}

func TestWeightArtifact_ImplementsArtifact(t *testing.T) {
	desc := v1.Descriptor{
		Digest: v1.Hash{Algorithm: "sha256", Hex: "def456"},
		Size:   4096,
	}
	layers := []packedLayer{
		{
			TarPath:   "/tmp/layer-0.tar.gz",
			Digest:    v1.Hash{Algorithm: "sha256", Hex: "aaa"},
			Size:      15000,
			MediaType: mediaTypeOCILayerTarGzip,
		},
	}
	entry := lockfile.WeightLockEntry{Name: "my-weights", Target: "/src/weights"}
	artifact := newWeightArtifact(entry, desc, layers)

	var _ Artifact = artifact // compile-time interface check

	require.Equal(t, ArtifactTypeWeight, artifact.Type())
	require.Equal(t, "my-weights", artifact.Name())
	require.Equal(t, desc, artifact.Descriptor())
}

func TestWeightArtifact_Fields(t *testing.T) {
	desc := v1.Descriptor{
		Digest: v1.Hash{Algorithm: "sha256", Hex: "def456"},
		Size:   4096,
	}
	layers := []packedLayer{
		{
			TarPath:   "/tmp/layer-0.tar",
			Digest:    v1.Hash{Algorithm: "sha256", Hex: "bbb"},
			Size:      2048,
			MediaType: mediaTypeOCILayerTar,
		},
	}
	entry := lockfile.WeightLockEntry{Name: "my-weights", Target: "/src/weights"}
	artifact := newWeightArtifact(entry, desc, layers)

	require.Equal(t, "/src/weights", artifact.Entry.Target)
	require.Equal(t, layers, artifact.Layers)
	require.Empty(t, artifact.Entry.SetDigest)
}

func TestWeightMediaTypeConstants(t *testing.T) {
	// The artifactType on the manifest is the only v1 media type with a
	// Cog-specific name; layers use standard OCI types.
	require.Equal(t, "application/vnd.cog.weight.v1", MediaTypeWeightArtifact)
	require.Equal(t, "application/vnd.oci.image.layer.v1.tar", mediaTypeOCILayerTar)
	require.Equal(t, "application/vnd.oci.image.layer.v1.tar+gzip", mediaTypeOCILayerTarGzip)
}
