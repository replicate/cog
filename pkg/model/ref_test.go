package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseRef(t *testing.T) {
	tests := []struct {
		name          string
		ref           string
		opts          []ParseOption
		wantRegistry  string
		wantRepo      string
		wantTag       string
		wantDigest    string
		wantReplicate bool
		wantErr       bool
		errContains   string
	}{
		{
			name:          "basic tag",
			ref:           "nginx:latest",
			wantRegistry:  "index.docker.io",
			wantRepo:      "library/nginx",
			wantTag:       "latest",
			wantReplicate: false,
		},
		{
			name:          "implicit latest tag",
			ref:           "nginx",
			wantRegistry:  "index.docker.io",
			wantRepo:      "library/nginx",
			wantTag:       "latest",
			wantReplicate: false,
		},
		{
			name:          "replicate registry",
			ref:           "r8.im/user/model:v1",
			wantRegistry:  "r8.im",
			wantRepo:      "user/model",
			wantTag:       "v1",
			wantReplicate: true,
		},
		{
			name:          "replicate registry implicit latest",
			ref:           "r8.im/user/model",
			wantRegistry:  "r8.im",
			wantRepo:      "user/model",
			wantTag:       "latest",
			wantReplicate: true,
		},
		{
			name:          "non-replicate registry",
			ref:           "ghcr.io/foo/bar:v1",
			wantRegistry:  "ghcr.io",
			wantRepo:      "foo/bar",
			wantTag:       "v1",
			wantReplicate: false,
		},
		{
			name:          "digest reference",
			ref:           "nginx@sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			wantRegistry:  "index.docker.io",
			wantRepo:      "library/nginx",
			wantTag:       "",
			wantDigest:    "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			wantReplicate: false,
		},
		{
			name:          "replicate with digest",
			ref:           "r8.im/user/model@sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			wantRegistry:  "r8.im",
			wantRepo:      "user/model",
			wantTag:       "",
			wantDigest:    "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			wantReplicate: true,
		},
		{
			name:          "custom registry with port",
			ref:           "localhost:5000/myimage:test",
			opts:          []ParseOption{Insecure()},
			wantRegistry:  "localhost:5000",
			wantRepo:      "myimage",
			wantTag:       "test",
			wantReplicate: false,
		},
		{
			name:          "with default registry option",
			ref:           "user/model:v1",
			opts:          []ParseOption{WithDefaultRegistry("r8.im")},
			wantRegistry:  "r8.im",
			wantRepo:      "user/model",
			wantTag:       "v1",
			wantReplicate: true,
		},
		{
			name:          "with default tag option",
			ref:           "nginx",
			opts:          []ParseOption{WithDefaultTag("stable")},
			wantRegistry:  "index.docker.io",
			wantRepo:      "library/nginx",
			wantTag:       "stable",
			wantReplicate: false,
		},
		{
			name:        "invalid reference",
			ref:         ":::invalid",
			wantErr:     true,
			errContains: `invalid image reference ":::invalid"`,
		},
		{
			name:        "empty reference",
			ref:         "",
			wantErr:     true,
			errContains: `invalid image reference ""`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := ParseRef(tt.ref, tt.opts...)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					require.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			require.NotNil(t, parsed)

			require.Equal(t, tt.ref, parsed.Original, "Original should preserve input")
			require.Equal(t, tt.wantRegistry, parsed.Registry(), "Registry mismatch")
			require.Equal(t, tt.wantRepo, parsed.Repository(), "Repository mismatch")
			require.Equal(t, tt.wantTag, parsed.Tag(), "Tag mismatch")
			require.Equal(t, tt.wantDigest, parsed.Digest(), "Digest mismatch")
			require.Equal(t, tt.wantReplicate, parsed.IsReplicate(), "IsReplicate mismatch")
		})
	}
}

func TestParsedRef_IsTag(t *testing.T) {
	tagRef, err := ParseRef("nginx:latest")
	require.NoError(t, err)
	require.True(t, tagRef.IsTag())
	require.False(t, tagRef.IsDigest())

	digestRef, err := ParseRef("nginx@sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")
	require.NoError(t, err)
	require.False(t, digestRef.IsTag())
	require.True(t, digestRef.IsDigest())
}

func TestParsedRef_String(t *testing.T) {
	tests := []struct {
		name    string
		ref     string
		wantStr string
	}{
		{
			name:    "bare image gets fully qualified",
			ref:     "nginx",
			wantStr: "index.docker.io/library/nginx:latest",
		},
		{
			name:    "replicate ref",
			ref:     "r8.im/user/model:v1",
			wantStr: "r8.im/user/model:v1",
		},
		{
			name:    "digest ref",
			ref:     "nginx@sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			wantStr: "index.docker.io/library/nginx@sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := ParseRef(tt.ref)
			require.NoError(t, err)
			require.Equal(t, tt.wantStr, parsed.String())
		})
	}
}

func TestParseOptions(t *testing.T) {
	t.Run("multiple options can be combined", func(t *testing.T) {
		parsed, err := ParseRef("myimage",
			WithDefaultRegistry("r8.im"),
			WithDefaultTag("v2"),
		)
		require.NoError(t, err)
		require.Equal(t, "r8.im", parsed.Registry())
		require.Equal(t, "v2", parsed.Tag())
		require.True(t, parsed.IsReplicate())
	})

	t.Run("insecure allows localhost registries", func(t *testing.T) {
		parsed, err := ParseRef("localhost:5000/test:v1", Insecure())
		require.NoError(t, err)
		require.Equal(t, "localhost:5000", parsed.Registry())
		require.Equal(t, "test", parsed.Repository())
		require.Equal(t, "v1", parsed.Tag())
	})
}
