package model

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/replicate/cog/pkg/config"
)

func TestBuildOptions_WithDefaults_ImageName(t *testing.T) {
	src := &Source{
		Config:     &config.Config{Build: &config.Build{}},
		ProjectDir: "/path/to/my-project",
	}

	opts := BuildOptions{}
	opts = opts.WithDefaults(src)

	// config.DockerImageName normalizes the name
	require.Equal(t, "cog-my-project", opts.ImageName)
}

func TestBuildOptions_WithDefaults_PreservesExplicitImageName(t *testing.T) {
	src := &Source{
		Config:     &config.Config{Build: &config.Build{}},
		ProjectDir: "/path/to/my-project",
	}

	opts := BuildOptions{ImageName: "my-custom-image"}
	opts = opts.WithDefaults(src)

	require.Equal(t, "my-custom-image", opts.ImageName)
}

func TestBuildOptions_WithDefaults_ProgressOutput(t *testing.T) {
	src := &Source{
		Config:     &config.Config{Build: &config.Build{}},
		ProjectDir: "/path/to/project",
	}

	opts := BuildOptions{}
	opts = opts.WithDefaults(src)

	require.Equal(t, "auto", opts.ProgressOutput)
}

func TestBuildOptions_WithDefaults_PreservesExplicitProgressOutput(t *testing.T) {
	src := &Source{
		Config:     &config.Config{Build: &config.Build{}},
		ProjectDir: "/path/to/project",
	}

	opts := BuildOptions{ProgressOutput: "plain"}
	opts = opts.WithDefaults(src)

	require.Equal(t, "plain", opts.ProgressOutput)
}

func TestBuildOptions_WithDefaults_FastFromConfig(t *testing.T) {
	src := &Source{
		Config: &config.Config{
			Build: &config.Build{Fast: true},
		},
		ProjectDir: "/path/to/project",
	}

	opts := BuildOptions{}
	opts = opts.WithDefaults(src)

	require.True(t, opts.Fast)
}

func TestBuildOptions_WithDefaults_ExplicitFastOverridesConfig(t *testing.T) {
	// When Fast is explicitly set to true in options, it stays true
	// even if config has Fast: false
	src := &Source{
		Config: &config.Config{
			Build: &config.Build{Fast: false},
		},
		ProjectDir: "/path/to/project",
	}

	opts := BuildOptions{Fast: true}
	opts = opts.WithDefaults(src)

	require.True(t, opts.Fast)
}

func TestBuildOptions_WithDefaults_NilBuildConfig(t *testing.T) {
	src := &Source{
		Config:     &config.Config{Build: nil},
		ProjectDir: "/path/to/project",
	}

	opts := BuildOptions{}
	opts = opts.WithDefaults(src)

	// Should not panic and should apply other defaults
	require.Equal(t, "cog-project", opts.ImageName)
	require.Equal(t, "auto", opts.ProgressOutput)
	require.False(t, opts.Fast)
}

func TestBuildOptions_WithDefaults_NilConfig(t *testing.T) {
	src := &Source{
		Config:     nil,
		ProjectDir: "/path/to/project",
	}

	opts := BuildOptions{}
	opts = opts.WithDefaults(src)

	// Should not panic and should apply other defaults
	require.Equal(t, "cog-project", opts.ImageName)
	require.Equal(t, "auto", opts.ProgressOutput)
	require.False(t, opts.Fast)
}

func TestBuildOptions_AllFieldsPreserved(t *testing.T) {
	src := &Source{
		Config:     &config.Config{Build: &config.Build{}},
		ProjectDir: "/path/to/project",
	}

	useCogBase := true
	opts := BuildOptions{
		ImageName:        "my-image",
		NoCache:          true,
		SeparateWeights:  true,
		Fast:             true,
		Strip:            true,
		Precompile:       true,
		LocalImage:       true,
		PipelinesImage:   true,
		UseCudaBaseImage: "true",
		UseCogBaseImage:  &useCogBase,
		Secrets:          []string{"secret1", "secret2"},
		ProgressOutput:   "tty",
		Annotations:      map[string]string{"key": "value"},
		SchemaFile:       "/path/to/schema.json",
		DockerfileFile:   "/path/to/Dockerfile",
	}

	result := opts.WithDefaults(src)

	require.Equal(t, "my-image", result.ImageName)
	require.True(t, result.NoCache)
	require.True(t, result.SeparateWeights)
	require.True(t, result.Fast)
	require.True(t, result.Strip)
	require.True(t, result.Precompile)
	require.True(t, result.LocalImage)
	require.True(t, result.PipelinesImage)
	require.Equal(t, "true", result.UseCudaBaseImage)
	require.NotNil(t, result.UseCogBaseImage)
	require.True(t, *result.UseCogBaseImage)
	require.Equal(t, []string{"secret1", "secret2"}, result.Secrets)
	require.Equal(t, "tty", result.ProgressOutput)
	require.Equal(t, map[string]string{"key": "value"}, result.Annotations)
	require.Equal(t, "/path/to/schema.json", result.SchemaFile)
	require.Equal(t, "/path/to/Dockerfile", result.DockerfileFile)
}
